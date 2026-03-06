/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"

	edgecloudlabsv1alpha1 "github.com/felipevicens/backend-evaluation-operator/api/v1alpha1"
)

const (
	// requeueDelay is the default delay for requeuing when waiting for resources.
	requeueDelay = 15 * time.Second
	// failedRequeueDelay is the delay for requeuing after a failure.
	failedRequeueDelay = 30 * time.Second
)

var (
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "llmbackend_reconcile_duration_seconds",
			Help:    "Duration of LLMBackend reconciliation in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"phase"},
	)
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llmbackend_reconcile_errors_total",
			Help: "Total number of LLMBackend reconciliation errors.",
		},
		[]string{"phase"},
	)
	phaseTransitions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llmbackend_phase_transitions_total",
			Help: "Total number of phase transitions.",
		},
		[]string{"from", "to"},
	)
)

func init() {
	metrics.Registry.MustRegister(reconcileDuration, reconcileErrors, phaseTransitions)
}

// LLMBackendReconciler reconciles a LLMBackend object.
type LLMBackendReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Clientset kubernetes.Interface
}

// evaluationOutput represents the JSON output from the MMLU evaluation job.
type evaluationOutput struct {
	Model            string             `json:"model"`
	OverallAccuracy  float64            `json:"overall_accuracy"`
	AvgResponseTime  float64            `json:"avg_response_time"`
	TokensPerSecond  float64            `json:"tokensPerSecond"`
	CategoryAccuracy map[string]float64 `json:"category_accuracy"`
}

// +kubebuilder:rbac:groups=edgecloudlabs.edgecloudlabs.com,resources=llmbackends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=edgecloudlabs.edgecloudlabs.com,resources=llmbackends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=edgecloudlabs.edgecloudlabs.com,resources=llmbackends/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=agentgateway.dev,resources=agentgatewaybackends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the phase-based state machine for LLMBackend.
func (r *LLMBackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()

	var eval edgecloudlabsv1alpha1.LLMBackend
	if err := r.Get(ctx, req.NamespacedName, &eval); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	currentPhase := string(eval.Status.Phase)
	defer func() {
		reconcileDuration.WithLabelValues(currentPhase).Observe(time.Since(startTime).Seconds())
	}()

	log.Info("Reconciling LLMBackend", "phase", eval.Status.Phase, "model", eval.Spec.Model)

	switch eval.Status.Phase {
	case edgecloudlabsv1alpha1.PhaseEmpty:
		return r.ensureBackend(ctx, &eval)

	case edgecloudlabsv1alpha1.PhaseBackendCreated:
		if eval.Spec.TriggerEvaluation != nil && !*eval.Spec.TriggerEvaluation {
			log.Info("Evaluation not triggered, skipping job creation")
			return ctrl.Result{}, nil
		}
		return r.ensureEvaluationJob(ctx, &eval)

	case edgecloudlabsv1alpha1.PhaseEvaluating:
		return r.checkJobStatus(ctx, &eval)

	case edgecloudlabsv1alpha1.PhaseEvaluated:
		log.Info("Evaluation complete", "model", eval.Spec.Model)
		return ctrl.Result{}, nil

	case edgecloudlabsv1alpha1.PhaseFailed:
		log.Info("Evaluation in Failed state, no action taken", "model", eval.Spec.Model)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// ensureBackend creates the AgentGatewayBackend CR if it doesn't exist.
func (r *LLMBackendReconciler) ensureBackend(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	backendName := eval.Spec.Model
	log.Info("Ensuring AgentGatewayBackend", "name", backendName)

	// Build the AgentGatewayBackend as an unstructured object
	backend := &unstructured.Unstructured{}
	backend.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "agentgateway.dev",
		Version: "v1alpha1",
		Kind:    "AgentgatewayBackend",
	})
	backend.SetName(backendName)
	backend.SetNamespace(eval.Namespace)

	// Check if it already exists
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(backend.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Name: backendName, Namespace: eval.Namespace}, existing)
	if err == nil {
		// Already exists
		log.Info("AgentGatewayBackend already exists", "name", backendName)
	} else if errors.IsNotFound(err) {
		var spec map[string]interface{}

		if eval.Spec.Endpoint != "" {
			// Parse the endpoint URL to extract host, port, and path
			parsedURL, err := url.Parse(eval.Spec.Endpoint)
			if err != nil {
				log.Error(err, "Failed to parse endpoint URL", "endpoint", eval.Spec.Endpoint)
				return r.setFailed(ctx, eval, fmt.Sprintf("Invalid endpoint URL: %v", err))
			}

			host := parsedURL.Hostname()
			portStr := parsedURL.Port()
			port := 80
			if portStr != "" {
				var err error
				portInt, err := strconv.Atoi(portStr)
				if err != nil {
					log.Error(err, "Failed to parse port from endpoint URL", "port", portStr)
				} else {
					port = portInt
				}
			} else if parsedURL.Scheme == "https" {
				port = 443
			}

			path := parsedURL.Path
			if path == "" || path == "/" {
				path = "/v1/chat/completions"
			} else if !strings.HasSuffix(path, "/chat/completions") {
				// If it's just /v1, append /chat/completions
				if strings.HasSuffix(path, "/v1") || strings.HasSuffix(path, "/v1/") {
					path = strings.TrimSuffix(path, "/") + "/chat/completions"
				}
			}

			// Build the spec for a single-model backend with endpoint
			spec = map[string]interface{}{
				"ai": map[string]interface{}{
					"provider": map[string]interface{}{
						"host": host,
						"port": port,
						"path": path,
						"openai": map[string]interface{}{
							"model": eval.Spec.Model,
						},
					},
				},
			}
		} else {
			// Build the spec for a public provider (no endpoint/host/port/path)
			spec = map[string]interface{}{
				"ai": map[string]interface{}{
					"provider": map[string]interface{}{
						"openai": map[string]interface{}{
							"model": eval.Spec.Model,
						},
					},
				},
			}
		}

		// Add auth policy referencing the API key secret if provided
		if eval.Spec.APIKeySecretRef != nil {
			spec["policies"] = map[string]interface{}{
				"auth": map[string]interface{}{
					"secretRef": map[string]interface{}{
						"name": eval.Spec.APIKeySecretRef.Name,
					},
				},
			}
		}

		backend.Object["spec"] = spec

		// Set OwnerReference
		if err := controllerutil.SetControllerReference(eval, backend, r.Scheme); err != nil {
			log.Error(err, "Failed to set OwnerReference on AgentGatewayBackend")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, backend); err != nil {
			log.Error(err, "Failed to create AgentGatewayBackend", "name", backendName)
			reconcileErrors.WithLabelValues(string(edgecloudlabsv1alpha1.PhaseEmpty)).Inc()
			r.Recorder.Eventf(eval, corev1.EventTypeWarning, "BackendCreationFailed",
				"Failed to create AgentGatewayBackend %s: %v", backendName, err)
			return ctrl.Result{RequeueAfter: failedRequeueDelay}, err
		}

		log.Info("Created AgentGatewayBackend", "name", backendName)
		r.Recorder.Eventf(eval, corev1.EventTypeNormal, "BackendCreated",
			"Created AgentGatewayBackend %s", backendName)
	} else {
		log.Error(err, "Failed to check AgentGatewayBackend existence")
		return ctrl.Result{}, err
	}

	// Ensure HTTPRoute
	if err := r.ensureHTTPRoute(ctx, eval, backendName); err != nil {
		r.Recorder.Eventf(eval, corev1.EventTypeWarning, "RouteCreationFailed",
			"Failed to create HTTPRoute %s-route: %v", backendName, err)
		return ctrl.Result{RequeueAfter: failedRequeueDelay}, err
	}

	// Transition to BackendCreated
	return r.updatePhase(ctx, eval, edgecloudlabsv1alpha1.PhaseBackendCreated, backendName, "")
}

// ensureHTTPRoute creates the HTTPRoute CR if it doesn't exist.
func (r *LLMBackendReconciler) ensureHTTPRoute(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend, backendName string) error {
	log := logf.FromContext(ctx)
	routeName := fmt.Sprintf("%s-route", eval.Spec.Model)

	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "HTTPRoute",
	})
	route.SetName(routeName)
	route.SetNamespace(eval.Namespace)

	// Check if already exists
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(route.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Name: routeName, Namespace: eval.Namespace}, existing)
	if err == nil {
		log.Info("HTTPRoute already exists", "name", routeName)
		return nil
	} else if !errors.IsNotFound(err) {
		log.Error(err, "Failed to check HTTPRoute existence")
		return err
	}

	spec := map[string]interface{}{
		"parentRefs": []interface{}{
			map[string]interface{}{
				"name":      "agentgateway-proxy",
				"namespace": "agentgateway-system",
			},
		},
		"rules": []interface{}{
			map[string]interface{}{
				"matches": []interface{}{
					map[string]interface{}{
						"headers": []interface{}{
							map[string]interface{}{
								"name":  "x-vsr-selected-model",
								"type":  "Exact",
								"value": eval.Spec.Model,
							},
						},
						"path": map[string]interface{}{
							"type":  "PathPrefix",
							"value": "/",
						},
					},
				},
				"backendRefs": []interface{}{
					map[string]interface{}{
						"group":     "agentgateway.dev",
						"kind":      "AgentgatewayBackend",
						"name":      eval.Spec.Model,
						"namespace": eval.Namespace,
					},
				},
			},
		},
	}
	route.Object["spec"] = spec

	// Set OwnerReference
	if err := controllerutil.SetControllerReference(eval, route, r.Scheme); err != nil {
		log.Error(err, "Failed to set OwnerReference on HTTPRoute")
		return err
	}

	if err := r.Create(ctx, route); err != nil {
		log.Error(err, "Failed to create HTTPRoute", "name", routeName)
		return err
	}

	log.Info("Created HTTPRoute", "name", routeName)
	r.Recorder.Eventf(eval, corev1.EventTypeNormal, "RouteCreated",
		"Created HTTPRoute %s", routeName)
	return nil
}

// ensureEvaluationJob creates the MMLU evaluation Kubernetes Job if it doesn't exist.
func (r *LLMBackendReconciler) ensureEvaluationJob(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	jobName := fmt.Sprintf("mmlu-%s", eval.Name)
	log.Info("Ensuring evaluation Job", "name", jobName)

	// Check if Job already exists
	var existingJob batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: eval.Namespace}, &existingJob)
	if err == nil {
		// Job already exists, transition to Evaluating
		log.Info("Evaluation Job already exists", "name", jobName)
		return r.updatePhase(ctx, eval, edgecloudlabsv1alpha1.PhaseEvaluating, "", jobName)
	}
	if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Determine the evaluation image
	evalImage := eval.Spec.EvaluationImage
	if evalImage == "" {
		evalImage = "fjvicens/mmlu-pro-eval-job:0.2"
	}

	// Build the Job
	backoffLimit := int32(0)
	ttl := int32(600)

	envVars := []corev1.EnvVar{
		{
			Name:  "EVAL_ENDPOINT",
			Value: eval.Spec.Endpoint,
		},
		{
			Name:  "EVAL_MODEL",
			Value: eval.Spec.Model,
		},
	}

	if eval.Spec.APIKeySecretRef != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name: "OPENAI_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: eval.Spec.APIKeySecretRef.Name,
					},
					Key: func() string {
						if eval.Spec.APIKeySecretRef.Key != "" {
							return eval.Spec.APIKeySecretRef.Key
						}
						return "Authorization" // Default for the evaluator job
					}(),
					Optional: ptrBool(true),
				},
			},
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: eval.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":            "model-backend-controller",
				"app.kubernetes.io/component":       "evaluator",
				"app.kubernetes.io/managed-by":      "model-backend-controller",
				"llmbackend.edgecloudlabs.com/name": eval.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":            "model-backend-controller",
						"app.kubernetes.io/component":       "evaluator",
						"llmbackend.edgecloudlabs.com/name": eval.Name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:            "evaluator",
							Image:           evalImage,
							ImagePullPolicy: corev1.PullAlways,
							Env:             envVars,
						},
					},
				},
			},
		},
	}

	// Set OwnerReference
	if err := controllerutil.SetControllerReference(eval, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set OwnerReference on Job")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "Failed to create evaluation Job", "name", jobName)
		reconcileErrors.WithLabelValues(string(edgecloudlabsv1alpha1.PhaseBackendCreated)).Inc()
		r.Recorder.Eventf(eval, corev1.EventTypeWarning, "JobCreationFailed",
			"Failed to create evaluation Job %s: %v", jobName, err)
		return ctrl.Result{RequeueAfter: failedRequeueDelay}, err
	}

	log.Info("Created evaluation Job", "name", jobName)
	r.Recorder.Eventf(eval, corev1.EventTypeNormal, "EvaluationStarted",
		"Created evaluation Job %s for model %s", jobName, eval.Spec.Model)

	// Transition to Evaluating
	return r.updatePhase(ctx, eval, edgecloudlabsv1alpha1.PhaseEvaluating, "", jobName)
}

// checkJobStatus checks the evaluation Job status and collects results on completion.
func (r *LLMBackendReconciler) checkJobStatus(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	jobName := eval.Status.JobName
	if jobName == "" {
		jobName = fmt.Sprintf("mmlu-%s", eval.Name)
	}

	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: eval.Namespace}, &job); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Job not found, may have been deleted", "name", jobName)
			return r.setFailed(ctx, eval, "Job not found: "+jobName)
		}
		return ctrl.Result{}, err
	}

	// Check Job conditions
	for _, condition := range job.Status.Conditions {
		switch condition.Type {
		case batchv1.JobComplete:
			if condition.Status == corev1.ConditionTrue {
				log.Info("Evaluation Job completed successfully", "name", jobName)
				return r.collectResults(ctx, eval, &job)
			}
		case batchv1.JobFailed:
			if condition.Status == corev1.ConditionTrue {
				msg := fmt.Sprintf("Evaluation Job %s failed: %s", jobName, condition.Message)
				log.Info("Evaluation Job failed", "name", jobName, "message", condition.Message)
				return r.setFailed(ctx, eval, msg)
			}
		}
	}

	// Job still active — requeue
	log.Info("Evaluation Job still running, requeueing", "name", jobName,
		"active", job.Status.Active, "succeeded", job.Status.Succeeded, "failed", job.Status.Failed)
	return ctrl.Result{RequeueAfter: requeueDelay}, nil
}

// collectResults reads the JSON output from the completed evaluation pod's logs.
func (r *LLMBackendReconciler) collectResults(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend, job *batchv1.Job) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Find the pod for this job
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(eval.Namespace),
		client.MatchingLabels{"batch.kubernetes.io/job-name": job.Name},
	); err != nil {
		log.Error(err, "Failed to list pods for Job")
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	if len(podList.Items) == 0 {
		log.Info("No pods found for completed Job, requeueing")
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	// Get logs from the first pod (should only be one for a non-parallel job)
	pod := podList.Items[0]
	log.Info("Reading evaluation results from pod logs", "pod", pod.Name)

	logStream, err := r.Clientset.CoreV1().Pods(eval.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "evaluator",
	}).Stream(ctx)
	if err != nil {
		log.Error(err, "Failed to get pod logs")
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}
	defer logStream.Close()

	var logBuf bytes.Buffer
	if _, err := io.Copy(&logBuf, logStream); err != nil {
		log.Error(err, "Failed to read pod logs")
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	// The evaluation script outputs JSON to stdout.
	// stderr contains diagnostics (progress bars, etc.), stdout is the JSON result.
	rawOutput := logBuf.String()

	var result evaluationOutput
	if err := json.Unmarshal([]byte(rawOutput), &result); err != nil {
		// Try to find JSON object in the output (in case there's extra output)
		jsonStart := bytes.IndexByte(logBuf.Bytes(), '{')
		jsonEnd := bytes.LastIndexByte(logBuf.Bytes(), '}')
		if jsonStart >= 0 && jsonEnd > jsonStart {
			jsonBytes := logBuf.Bytes()[jsonStart : jsonEnd+1]
			if err2 := json.Unmarshal(jsonBytes, &result); err2 != nil {
				log.Error(err2, "Failed to parse evaluation results JSON", "rawOutput", rawOutput)
				return r.setFailed(ctx, eval, fmt.Sprintf("Failed to parse evaluation results: %v", err2))
			}
		} else {
			log.Error(err, "No JSON found in evaluation output", "rawOutput", rawOutput)
			return r.setFailed(ctx, eval, fmt.Sprintf("No valid JSON in evaluation output: %v", err))
		}
	}

	// Validate results
	// If all metrics are zero or category accuracy is empty, it's likely a failed run
	// that produced a default JSON output.
	if (result.OverallAccuracy == 0 && result.TokensPerSecond == 0 && result.AvgResponseTime == 0) || len(result.CategoryAccuracy) == 0 {
		log.Error(nil, "Evaluation Job produced zeroed or empty results", "rawOutput", rawOutput)
		return r.setFailed(ctx, eval, "Evaluation produced empty or zeroed results. Check job logs for API errors.")
	}

	if result.OverallAccuracy < 0 || result.OverallAccuracy > 1 {
		log.Info("Warning: unexpected accuracy value", "accuracy", result.OverallAccuracy)
	}

	// Store results in status
	eval.Status.Results = &edgecloudlabsv1alpha1.EvaluationResults{
		OverallAccuracy:  fmt.Sprintf("%.4f", result.OverallAccuracy),
		TokensPerSecond:  fmt.Sprintf("%.2f", result.TokensPerSecond),
		AvgResponseTime:  fmt.Sprintf("%.4f", result.AvgResponseTime),
		CategoryAccuracy: make(map[string]string),
	}

	for cat, acc := range result.CategoryAccuracy {
		eval.Status.Results.CategoryAccuracy[cat] = fmt.Sprintf("%.4f", acc)
	}

	r.Recorder.Eventf(eval, corev1.EventTypeNormal, "EvaluationCompleted",
		"Model %s evaluation complete: accuracy=%.4f, tok/s=%.2f, avg_response_time=%.4fs",
		eval.Spec.Model, result.OverallAccuracy, result.TokensPerSecond, result.AvgResponseTime)

	log.Info("Evaluation results collected",
		"model", eval.Spec.Model,
		"accuracy", result.OverallAccuracy,
		"tokensPerSecond", result.TokensPerSecond,
		"avgResponseTime", result.AvgResponseTime)

	// Transition to Evaluated
	return r.updatePhase(ctx, eval, edgecloudlabsv1alpha1.PhaseEvaluated, "", "")
}

// updatePhase transitions the LLMBackend to a new phase and updates status.
func (r *LLMBackendReconciler) updatePhase(
	ctx context.Context,
	eval *edgecloudlabsv1alpha1.LLMBackend,
	newPhase edgecloudlabsv1alpha1.LLMBackendPhase,
	backendName string,
	jobName string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	oldPhase := eval.Status.Phase
	eval.Status.Phase = newPhase

	if backendName != "" {
		eval.Status.BackendName = backendName
	}
	if jobName != "" {
		eval.Status.JobName = jobName
	}

	// Update conditions
	conditionType := "Ready"
	conditionStatus := metav1.ConditionFalse
	reason := string(newPhase)
	message := fmt.Sprintf("LLMBackend is in phase %s", newPhase)

	if newPhase == edgecloudlabsv1alpha1.PhaseEvaluated {
		conditionStatus = metav1.ConditionTrue
		reason = "EvaluationComplete"
		message = "Evaluation completed successfully"
	} else if newPhase == edgecloudlabsv1alpha1.PhaseFailed {
		reason = "EvaluationFailed"
		message = "Evaluation failed"
	}

	if reason == "" {
		reason = "Pending"
	}

	meta.SetStatusCondition(&eval.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, eval); err != nil {
		log.Error(err, "Failed to update status", "phase", newPhase)
		return ctrl.Result{}, err
	}

	phaseTransitions.WithLabelValues(string(oldPhase), string(newPhase)).Inc()
	log.Info("Phase transition", "from", oldPhase, "to", newPhase)

	// Requeue to proceed with the next phase
	return ctrl.Result{Requeue: true}, nil
}

// setFailed transitions the LLMBackend to the Failed phase.
func (r *LLMBackendReconciler) setFailed(ctx context.Context, eval *edgecloudlabsv1alpha1.LLMBackend, message string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	eval.Status.Phase = edgecloudlabsv1alpha1.PhaseFailed
	reconcileErrors.WithLabelValues(string(eval.Status.Phase)).Inc()

	meta.SetStatusCondition(&eval.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "EvaluationFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, eval); err != nil {
		log.Error(err, "Failed to update status to Failed")
		return ctrl.Result{}, err
	}

	r.Recorder.Eventf(eval, corev1.EventTypeWarning, "EvaluationFailed", message)
	log.Info("LLMBackend set to Failed", "message", message)

	// Don't requeue — terminal state
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LLMBackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&edgecloudlabsv1alpha1.LLMBackend{}).
		Owns(&batchv1.Job{}).
		Named("llmbackend").
		Complete(r)
}

func ptrBool(b bool) *bool {
	return &b
}
