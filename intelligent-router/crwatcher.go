package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"gopkg.in/yaml.v2"
)

var intelligentRouterConfigGVR = schema.GroupVersionResource{
	Group:    "vllm.ai",
	Version:  "v1alpha1",
	Resource: "intelligentrouterconfigs",
}

// getPodNamespace returns the namespace the pod is running in.
// It reads /var/run/secrets/kubernetes.io/serviceaccount/namespace (in-cluster),
// then falls back to the NAMESPACE env var, then to "default".
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

// startCRWatcher starts a goroutine that polls for IntelligentRouterConfig CRs
// in the pod's own namespace every 10 seconds and hot-reloads the server config
// on any change. It expects exactly one CR in the namespace; if multiple exist
// it uses the first one and logs a warning.
func startCRWatcher(ctx context.Context, srv *server) error {
	namespace := getPodNamespace()

	// Build Kubernetes config: try in-cluster first, fall back to kubeconfig.
	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("[crwatcher] not running in-cluster (%v), falling back to kubeconfig", err)
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

	var lastResourceVersion string

	apply := func() {
		rv, err := applyFirstCR(ctx, dynClient, namespace, &lastResourceVersion, srv)
		if err != nil {
			log.Printf("[crwatcher] reload failed: %v", err)
			return
		}
		if rv != "" {
			lastResourceVersion = rv
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

	log.Printf("[crwatcher] watching IntelligentRouterConfig in namespace %s (poll every 10s)", namespace)
	return nil
}

// applyFirstCR lists all IntelligentRouterConfig CRs in the namespace, picks the
// first one, and hot-reloads the server state if its resourceVersion changed.
// Returns the resourceVersion of the CR used, or "" if unchanged or not found.
func applyFirstCR(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	lastRV *string,
	srv *server,
) (string, error) {
	list, err := client.Resource(intelligentRouterConfigGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list IntelligentRouterConfig in %s: %w", namespace, err)
	}

	if len(list.Items) == 0 {
		log.Printf("[crwatcher] no IntelligentRouterConfig found in namespace %s — skipping reload", namespace)
		return "", nil
	}

	if len(list.Items) > 1 {
		log.Printf("[crwatcher] warning: %d IntelligentRouterConfig CRs found in %s, using %q",
			len(list.Items), namespace, list.Items[0].GetName())
	}

	u := &list.Items[0]
	rv := u.GetResourceVersion()
	if rv == *lastRV {
		return "", nil // unchanged
	}

	// Convert unstructured → JSON → YAML struct.
	// The K8s dynamic client returns the object as JSON-compatible maps.
	// gopkg.in/yaml.v2 can parse JSON (JSON is valid YAML 1.1), and our
	// IntelligentRouterConfig struct uses camelCase yaml tags matching the CR.
	jsonBytes, err := json.Marshal(u.Object)
	if err != nil {
		return "", fmt.Errorf("marshal unstructured: %w", err)
	}

	cr := &IntelligentRouterConfig{}
	if err := yaml.Unmarshal(jsonBytes, cr); err != nil {
		return "", fmt.Errorf("unmarshal IntelligentRouterConfig: %w", err)
	}

	cfg := convertCRToConfig(cr)

	kc, err := NewKeywordClassifier(cfg.KeywordRules)
	if err != nil {
		return "", fmt.Errorf("keyword classifier init: %w", err)
	}

	var mlc *MLClassifier
	if cfg.MLClassifier.Enabled {
		var mlErr error
		mlc, mlErr = NewMLClassifier(
			cfg.MLClassifier.ModelPath,
			cfg.MLClassifier.MappingPath,
			cfg.MLClassifier.NumClasses,
			cfg.MLClassifier.UseCPU,
			cfg.MLClassifier.Threshold,
		)
		if mlErr != nil {
			log.Printf("[crwatcher] ML classifier disabled: %v", mlErr)
		}
	}

	state := &routerState{config: cfg, classifier: kc, mlClassifier: mlc}

	// Ensure latency tracker has entries for all models.
	for modelName, m := range cfg.Models {
		srv.latencyTracker.EnsureModel(modelName, m.InitialAverageLatencyMs)
	}
	srv.current.Store(state)

	modelNames := make([]string, 0, len(cfg.Models))
	for n := range cfg.Models {
		modelNames = append(modelNames, n)
	}
	log.Printf("[crwatcher] loaded %q rv=%s default=%s models=%v keyword_rules=%d ml_enabled=%v",
		u.GetName(), rv, cfg.DefaultModel, modelNames, len(cfg.KeywordRules), cfg.MLClassifier.Enabled)

	return rv, nil
}
