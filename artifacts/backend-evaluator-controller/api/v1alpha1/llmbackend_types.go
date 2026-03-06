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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LLMBackendPhase represents the current phase of the evaluation lifecycle.
// +kubebuilder:validation:Enum="";"BackendCreated";"Evaluating";"Evaluated";"Failed"
type LLMBackendPhase string

const (
	PhaseEmpty          LLMBackendPhase = ""
	PhaseBackendCreated LLMBackendPhase = "BackendCreated"
	PhaseEvaluating     LLMBackendPhase = "Evaluating"
	PhaseEvaluated      LLMBackendPhase = "Evaluated"
	PhaseFailed         LLMBackendPhase = "Failed"
)

// DeploymentType represents the type of deployment for the model.
// +kubebuilder:validation:Enum="local";"remote"
type DeploymentType string

const (
	DeploymentLocal  DeploymentType = "local"
	DeploymentRemote DeploymentType = "remote"
)

// SecretKeySelector references a key within a Kubernetes Secret.
type SecretKeySelector struct {
	// name is the name of the Secret in the same namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// key is the key within the Secret data to use.
	// +optional
	Key string `json:"key,omitempty"`
}

// EvaluationResults holds the structured results of an MMLU evaluation.
type EvaluationResults struct {
	// overallAccuracy is the overall accuracy of the model across all categories.
	// +optional
	OverallAccuracy string `json:"overallAccuracy,omitempty"`

	// tokensPerSecond is the average tokens per second during evaluation.
	// +optional
	TokensPerSecond string `json:"tokensPerSecond,omitempty"`

	// avgResponseTime is the average response time in seconds.
	// +optional
	AvgResponseTime string `json:"avgResponseTime,omitempty"`

	// categoryAccuracy contains accuracy scores per domain/category.
	// +optional
	CategoryAccuracy map[string]string `json:"categoryAccuracy,omitempty"`
}

// LLMBackendSpec defines the desired state of LLMBackend.
type LLMBackendSpec struct {
	// deployment specifies if the model is hosted locally or remotely.
	// +kubebuilder:validation:Enum="local";"remote"
	// +required
	Deployment DeploymentType `json:"deployment"`

	// model is the name/identifier of the AI model to evaluate.
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// endpoint is the URL of the vLLM-compatible OpenAI API endpoint.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// apiKeySecretRef references the Secret containing the API key for the endpoint.
	// +optional
	APIKeySecretRef *SecretKeySelector `json:"apiKeySecretRef,omitempty"`

	// triggerEvaluation controls whether the evaluation Job should be created.
	// +kubebuilder:default=true
	// +optional
	TriggerEvaluation *bool `json:"triggerEvaluation,omitempty"`

	// evaluationImage is the container image to use for the evaluation Job.
	// +kubebuilder:default="fjvicens/mmlu-pro-eval-job:0.2"
	// +optional
	EvaluationImage string `json:"evaluationImage,omitempty"`
}

// LLMBackendStatus defines the observed state of LLMBackend.
type LLMBackendStatus struct {
	// phase represents the current phase of the evaluation lifecycle.
	// +optional
	Phase LLMBackendPhase `json:"phase,omitempty"`

	// backendName is the name of the created AgentGatewayBackend resource.
	// +optional
	BackendName string `json:"backendName,omitempty"`

	// jobName is the name of the created evaluation Job.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// results contains the structured evaluation results.
	// +optional
	Results *EvaluationResults `json:"results,omitempty"`

	// conditions represent the current state of the LLMBackend resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Deployment",type=string,JSONPath=`.spec.deployment`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Backend",type=string,JSONPath=`.status.backendName`
// +kubebuilder:printcolumn:name="Accuracy",type=string,JSONPath=`.status.results.overallAccuracy`
// +kubebuilder:printcolumn:name="Latency(s)",type=string,JSONPath=`.status.results.avgResponseTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LLMBackend is the Schema for the llmbackends API.
// It orchestrates the creation of an AgentGatewayBackend, triggers an MMLU
// evaluation Job, and collects structured results for external agent consumption.
type LLMBackend struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LLMBackend
	// +required
	Spec LLMBackendSpec `json:"spec"`

	// status defines the observed state of LLMBackend
	// +optional
	Status LLMBackendStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMBackendList contains a list of LLMBackend
type LLMBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMBackend{}, &LLMBackendList{})
}
