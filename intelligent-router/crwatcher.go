package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var llmBackendGVR = schema.GroupVersionResource{
	Group:    "edgecloudlabs.edgecloudlabs.com",
	Version:  "v1alpha1",
	Resource: "llmbackends",
}

// startLLMBackendWatcher starts a goroutine that polls for LLMBackend CRs
// across all namespaces every 10 seconds and hot-reloads the server config
// when any backend's resourceVersion changes.
func startLLMBackendWatcher(ctx context.Context, srv *server, optimizationTarget string) error {
	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("[llmwatcher] not running in-cluster (%v), falling back to kubeconfig", err)
		k8sCfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to build k8s config: %w", err)
		}
	}

	dynClient, err := dynamic.NewForConfig(k8sCfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	var lastFingerprint string

	apply := func() {
		fp, err := applyLLMBackends(ctx, dynClient, &lastFingerprint, srv, optimizationTarget)
		if err != nil {
			log.Printf("[llmwatcher] reload failed: %v", err)
			return
		}
		if fp != "" {
			lastFingerprint = fp
		}
	}

	// Load once at startup (blocking so the server starts with valid config).
	apply()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				apply()
			}
		}
	}()

	log.Printf("[llmwatcher] watching LLMBackend CRs across all namespaces (poll every 10s)")
	return nil
}

// applyLLMBackends lists all LLMBackend CRs cluster-wide, extracts evaluated
// backends, and hot-reloads the server state if the set has changed.
func applyLLMBackends(
	ctx context.Context,
	client dynamic.Interface,
	lastFingerprint *string,
	srv *server,
	optimizationTarget string,
) (string, error) {
	list, err := client.Resource(llmBackendGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list LLMBackend: %w", err)
	}

	var backends []LLMBackendData
	var rvParts []string

	for _, item := range list.Items {
		b, rv, ok := parseLLMBackend(&item)
		if !ok {
			continue
		}
		backends = append(backends, b)
		rvParts = append(rvParts, item.GetName()+"="+rv)
	}

	if len(backends) == 0 {
		log.Printf("[llmwatcher] no evaluated LLMBackend CRs found — skipping reload")
		return "", nil
	}

	// Build a fingerprint from sorted name=rv pairs to detect changes.
	sort.Strings(rvParts)
	fingerprint := strings.Join(rvParts, ",")
	if fingerprint == *lastFingerprint {
		return "", nil // unchanged
	}

	cfg := convertLLMBackendsToConfig(backends, optimizationTarget)

	kc, err := NewKeywordClassifier(cfg.KeywordRules)
	if err != nil {
		return "", fmt.Errorf("keyword classifier init: %w", err)
	}

	var mlc *MLClassifier
	if cfg.MLClassifier.Enabled {
		mlc, err = NewMLClassifier(
			cfg.MLClassifier.ModelPath,
			cfg.MLClassifier.MappingPath,
			cfg.MLClassifier.NumClasses,
			cfg.MLClassifier.UseCPU,
			cfg.MLClassifier.Threshold,
		)
		if err != nil {
			log.Printf("[llmwatcher] ML classifier disabled: %v", err)
			mlc = nil
		}
	}

	state := &routerState{config: cfg, classifier: kc, mlClassifier: mlc}

	for modelName, m := range cfg.Models {
		srv.latencyTracker.EnsureModel(modelName, m.InitialAverageLatencyMs)
	}
	srv.current.Store(state)

	modelNames := make([]string, 0, len(cfg.Models))
	for n := range cfg.Models {
		modelNames = append(modelNames, n)
	}
	log.Printf("[llmwatcher] loaded %d LLMBackend(s): models=%v default=%s target=%s",
		len(backends), modelNames, cfg.DefaultModel, optimizationTarget)

	return fingerprint, nil
}

// parseLLMBackend extracts LLMBackendData from an unstructured LLMBackend CR.
// Returns (data, resourceVersion, ok). ok is false if the backend is not evaluated.
func parseLLMBackend(u *unstructured.Unstructured) (LLMBackendData, string, bool) {
	rv := u.GetResourceVersion()

	status, ok := u.Object["status"].(map[string]interface{})
	if !ok {
		return LLMBackendData{}, rv, false
	}
	phase, _ := status["phase"].(string)
	if phase != "Evaluated" {
		return LLMBackendData{}, rv, false
	}
	results, ok := status["results"].(map[string]interface{})
	if !ok {
		return LLMBackendData{}, rv, false
	}

	spec, _ := u.Object["spec"].(map[string]interface{})
	modelName, _ := spec["model"].(string)
	if modelName == "" {
		modelName = u.GetName()
	}

	b := LLMBackendData{Name: modelName}

	if v, ok := results["avgResponseTime"].(string); ok {
		b.AvgResponseTime, _ = strconv.ParseFloat(v, 64)
	}
	if v, ok := results["tokensPerSecond"].(string); ok {
		b.TokensPerSecond, _ = strconv.ParseFloat(v, 64)
	}

	if pricing, ok := results["pricing"].(map[string]interface{}); ok {
		if v, ok := pricing["prompt"].(string); ok {
			b.PromptCost, _ = strconv.ParseFloat(v, 64)
		}
		if v, ok := pricing["completion"].(string); ok {
			b.CompletionCost, _ = strconv.ParseFloat(v, 64)
		}
	}

	if catAcc, ok := results["categoryAccuracy"].(map[string]interface{}); ok {
		b.CategoryAccuracy = make(map[string]float64, len(catAcc))
		for cat, val := range catAcc {
			if s, ok := val.(string); ok {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					b.CategoryAccuracy[cat] = f
				}
			}
		}
	}

	return b, rv, true
}

// getPodNamespace returns the namespace the pod is running in.
func getPodNamespace() string {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			return ns
		}
	}
	if ns := os.Getenv("NAMESPACE"); ns != "" {
		return ns
	}
	return "default"
}
