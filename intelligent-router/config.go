package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// ── Shared runtime types ───────────────────────────────────────────────────────

// RouterConfig is the unified routing configuration used at runtime.
// It is produced either from the new IntelligentRouterConfig CR (LoadFromCR)
// or from a legacy combined config.yaml (LoadConfig).
type RouterConfig struct {
	Models       map[string]ModelConfig `yaml:"models"`
	Weights      ScoringWeights         `yaml:"weights"`
	KeywordRules []KeywordRule          `yaml:"keyword_rules"` // legacy / optional
	DefaultModel string                 `yaml:"default_model"`
	TokenBudget  TokenBudgetConfig      `yaml:"token_budget"`
	MLClassifier MLClassifierConfig     `yaml:"ml_classifier"`
}

// ModelConfig is the runtime metadata for one backend model.
type ModelConfig struct {
	// QualityScores maps domain name to quality score in [0,1].
	QualityScores           map[string]float64 `yaml:"quality_scores"`
	// CostScore is a normalised cost value in [0,1]. Higher = more expensive.
	CostScore               float64            `yaml:"cost_score"`
	// InitialAverageLatencyMs seeds the latency tracker for this model.
	InitialAverageLatencyMs float64            `yaml:"initial_average_latency_ms"`
	// Informational fields.
	EndpointType         string `yaml:"endpoint_type"`
	AIInfrastructureType string `yaml:"ai_infrastructure_type"`
}

// ScoringWeights holds the weight for each scoring dimension.
type ScoringWeights struct {
	Quality float64 `yaml:"quality"` // weight for quality score (positive contributor)
	Latency float64 `yaml:"latency"` // weight for latency score (penalty)
	Cost    float64 `yaml:"cost"`    // weight for cost score (penalty)
}

// TokenBudgetConfig configures per-API-key token tracking and routing pressure.
type TokenBudgetConfig struct {
	Enabled       bool  `yaml:"enabled"`
	Threshold     int64 `yaml:"threshold"`
	Quota         int64 `yaml:"quota"`
	WindowSeconds int64 `yaml:"window_seconds"`
}

// KeywordRule defines a rule for keyword-based domain classification (legacy).
type KeywordRule struct {
	Name          string   `yaml:"name"`
	Keywords      []string `yaml:"keywords"`
	Operator      string   `yaml:"operator"`
	CaseSensitive bool     `yaml:"case_sensitive"`
}

// MLClassifierConfig configures the ML domain classifier.
type MLClassifierConfig struct {
	Enabled     bool    `yaml:"enabled"`
	ModelPath   string  `yaml:"model_path"`
	MappingPath string  `yaml:"mapping_path"`
	NumClasses  int     `yaml:"num_classes"`
	UseCPU      bool    `yaml:"use_cpu"`
	Threshold   float64 `yaml:"threshold"`
}

// ── IntelligentRouterConfig CR (vllm.ai/v1alpha1) ────────────────────────────

// IntelligentRouterConfig is the unified CR that replaces IntelligentPool + IntelligentRoute.
type IntelligentRouterConfig struct {
	APIVersion string                     `yaml:"apiVersion"`
	Kind       string                     `yaml:"kind"`
	Metadata   CRMetadata                 `yaml:"metadata"`
	Spec       IntelligentRouterConfigSpec `yaml:"spec"`
}

// IntelligentRouterConfigSpec is the spec section of the CR.
type IntelligentRouterConfigSpec struct {
	Pool         RouterPool       `yaml:"pool"`
	Weights      CRScoringWeights `yaml:"weights"`
	TokenBudget  CRTokenBudget    `yaml:"tokenBudget"`
	MLClassifier CRMLClassifier   `yaml:"mlClassifier"`
	KeywordRules []CRKeywordRule  `yaml:"keywordRules"`
}

// CRKeywordRule is a keyword-based domain classification rule in the CR (camelCase tags).
type CRKeywordRule struct {
	Name          string   `yaml:"name"`
	Keywords      []string `yaml:"keywords"`
	Operator      string   `yaml:"operator"`
	CaseSensitive bool     `yaml:"caseSensitive"`
}

// CRMLClassifier configures the BERT-based ML domain classifier in the CR.
type CRMLClassifier struct {
	Enabled     bool    `yaml:"enabled"`
	ModelPath   string  `yaml:"modelPath"`
	MappingPath string  `yaml:"mappingPath"`
	NumClasses  int     `yaml:"numClasses"`
	UseCPU      bool    `yaml:"useCPU"`
	Threshold   float64 `yaml:"threshold"`
}

// RouterPool defines the available models and the default fallback.
type RouterPool struct {
	DefaultModel string        `yaml:"defaultModel"`
	Models       []RouterModel `yaml:"models"`
}

// RouterModel describes a single model entry in the pool.
type RouterModel struct {
	Name                    string             `yaml:"name"`
	InitialAverageLatencyMs float64            `yaml:"initialAverageLatencyMs"`
	QualityScores           map[string]float64 `yaml:"qualityScores"`
	CostScore               float64            `yaml:"costScore"`
	EndpointType            string             `yaml:"endpointType"`
	AIInfrastructureType    string             `yaml:"aiInfrastructureType"`
}

// CRScoringWeights uses camelCase YAML tags to match the Kubernetes CR convention.
type CRScoringWeights struct {
	Quality float64 `yaml:"quality"`
	Latency float64 `yaml:"latency"`
	Cost    float64 `yaml:"cost"`
}

// CRTokenBudget uses camelCase YAML tags to match the Kubernetes CR convention.
type CRTokenBudget struct {
	Enabled       bool  `yaml:"enabled"`
	Threshold     int64 `yaml:"threshold"`
	Quota         int64 `yaml:"quota"`
	WindowSeconds int64 `yaml:"windowSeconds"`
}

// CRMetadata holds standard Kubernetes object metadata.
type CRMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

// ── IntelligentPool + IntelligentRoute CRs (separate files) ──────────────────

// IntelligentPool is the CR for model pool configuration.
type IntelligentPool struct {
	Kind     string              `yaml:"kind"`
	Metadata CRMetadata          `yaml:"metadata"`
	Spec     IntelligentPoolSpec `yaml:"spec"`
}

// IntelligentPoolSpec holds the pool spec.
type IntelligentPoolSpec struct {
	DefaultModel string                `yaml:"defaultModel"`
	Models       []IntelligentPoolModel `yaml:"models"`
}

// IntelligentPoolModel describes a single model in the pool.
type IntelligentPoolModel struct {
	Name             string             `yaml:"name"`
	Pricing          IntelligentPricing `yaml:"pricing"`
	Domains          []string           `yaml:"domains"`
	AverageLatencyMs float64            `yaml:"averageLatencyMs"`
}

// IntelligentPricing holds per-token pricing information.
type IntelligentPricing struct {
	InputTokenPrice  float64 `yaml:"inputTokenPrice"`
	OutputTokenPrice float64 `yaml:"outputTokenPrice"`
}

// IntelligentRoute is the CR for routing signal configuration.
type IntelligentRoute struct {
	Kind     string               `yaml:"kind"`
	Metadata CRMetadata           `yaml:"metadata"`
	Spec     IntelligentRouteSpec `yaml:"spec"`
}

// IntelligentRouteSpec holds the route spec.
type IntelligentRouteSpec struct {
	Signals     RouteSignals     `yaml:"signals"`
	Weights     RouteWeights     `yaml:"weights"`
	TokenBudget RouteTokenBudget `yaml:"tokenBudget"`
}

// RouteSignals holds routing signal configuration.
type RouteSignals struct {
	Keywords []RouteKeyword `yaml:"keywords"`
}

// RouteKeyword is a keyword-based routing signal.
type RouteKeyword struct {
	Name          string   `yaml:"name"`
	Operator      string   `yaml:"operator"`
	CaseSensitive bool     `yaml:"caseSensitive"`
	Keywords      []string `yaml:"keywords"`
}

// RouteWeights configures scoring dimension weights.
type RouteWeights struct {
	Domain      float64 `yaml:"domain"`
	Latency     float64 `yaml:"latency"`
	Cost        float64 `yaml:"cost"`
	TokenBudget float64 `yaml:"tokenBudget"`
}

// RouteTokenBudget configures per-API-key token budget tracking.
type RouteTokenBudget struct {
	Threshold     int64 `yaml:"threshold"`
	Quota         int64 `yaml:"quota"`
	WindowSeconds int64 `yaml:"windowSeconds"`
}

// LoadFromPoolAndRoute loads an IntelligentPool and IntelligentRoute CR from
// separate YAML files and combines them into a RouterConfig.
func LoadFromPoolAndRoute(poolPath, routePath string) (*RouterConfig, error) {
	poolData, err := os.ReadFile(poolPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool file %s: %w", poolPath, err)
	}
	pool := &IntelligentPool{}
	if err := yaml.Unmarshal(poolData, pool); err != nil {
		return nil, fmt.Errorf("failed to parse IntelligentPool: %w", err)
	}

	routeData, err := os.ReadFile(routePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read route file %s: %w", routePath, err)
	}
	route := &IntelligentRoute{}
	if err := yaml.Unmarshal(routeData, route); err != nil {
		return nil, fmt.Errorf("failed to parse IntelligentRoute: %w", err)
	}

	return convertPoolAndRoute(pool, route), nil
}

// convertPoolAndRoute converts IntelligentPool + IntelligentRoute CRs to RouterConfig.
func convertPoolAndRoute(pool *IntelligentPool, route *IntelligentRoute) *RouterConfig {
	// Normalise cost by max input-token price across all models.
	var maxPrice float64
	for _, m := range pool.Spec.Models {
		if m.Pricing.InputTokenPrice > maxPrice {
			maxPrice = m.Pricing.InputTokenPrice
		}
	}
	if maxPrice == 0 {
		maxPrice = 1
	}

	models := make(map[string]ModelConfig, len(pool.Spec.Models))
	for _, m := range pool.Spec.Models {
		qs := make(map[string]float64, len(m.Domains))
		for _, d := range m.Domains {
			qs[d] = 1.0
		}
		models[m.Name] = ModelConfig{
			QualityScores:           qs,
			CostScore:               m.Pricing.InputTokenPrice / maxPrice,
			InitialAverageLatencyMs: m.AverageLatencyMs,
		}
	}

	defaultModel := pool.Spec.DefaultModel
	if defaultModel == "" && len(pool.Spec.Models) > 0 {
		defaultModel = pool.Spec.Models[0].Name
	}

	var keywordRules []KeywordRule
	for _, k := range route.Spec.Signals.Keywords {
		keywordRules = append(keywordRules, KeywordRule{
			Name:          k.Name,
			Keywords:      k.Keywords,
			Operator:      k.Operator,
			CaseSensitive: k.CaseSensitive,
		})
	}

	return &RouterConfig{
		Models:       models,
		DefaultModel: defaultModel,
		Weights: ScoringWeights{
			Quality: route.Spec.Weights.Domain,
			Latency: route.Spec.Weights.Latency,
			Cost:    route.Spec.Weights.Cost,
		},
		KeywordRules: keywordRules,
		TokenBudget: TokenBudgetConfig{
			Enabled:       route.Spec.TokenBudget.Threshold > 0,
			Threshold:     route.Spec.TokenBudget.Threshold,
			Quota:         route.Spec.TokenBudget.Quota,
			WindowSeconds: route.Spec.TokenBudget.WindowSeconds,
		},
	}
}

// LoadFromCR loads an IntelligentRouterConfig CR from a single YAML file.
func LoadFromCR(path string) (*RouterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read IntelligentRouterConfig file %s: %w", path, err)
	}
	cr := &IntelligentRouterConfig{}
	if err := yaml.Unmarshal(data, cr); err != nil {
		return nil, fmt.Errorf("failed to parse IntelligentRouterConfig: %w", err)
	}
	return convertCRToConfig(cr), nil
}

// convertCRToConfig converts an IntelligentRouterConfig CR to RouterConfig.
func convertCRToConfig(cr *IntelligentRouterConfig) *RouterConfig {
	models := make(map[string]ModelConfig, len(cr.Spec.Pool.Models))
	for _, m := range cr.Spec.Pool.Models {
		models[m.Name] = ModelConfig{
			QualityScores:           m.QualityScores,
			CostScore:               m.CostScore,
			InitialAverageLatencyMs: m.InitialAverageLatencyMs,
			EndpointType:            m.EndpointType,
			AIInfrastructureType:    m.AIInfrastructureType,
		}
	}
	defaultModel := cr.Spec.Pool.DefaultModel
	if defaultModel == "" {
		defaultModel = "default"
	}
	var keywordRules []KeywordRule
	for _, k := range cr.Spec.KeywordRules {
		keywordRules = append(keywordRules, KeywordRule{
			Name:          k.Name,
			Keywords:      k.Keywords,
			Operator:      k.Operator,
			CaseSensitive: k.CaseSensitive,
		})
	}

	return &RouterConfig{
		Models:       models,
		DefaultModel: defaultModel,
		KeywordRules: keywordRules,
		Weights: ScoringWeights{
			Quality: cr.Spec.Weights.Quality,
			Latency: cr.Spec.Weights.Latency,
			Cost:    cr.Spec.Weights.Cost,
		},
		TokenBudget: TokenBudgetConfig{
			Enabled:       cr.Spec.TokenBudget.Enabled,
			Threshold:     cr.Spec.TokenBudget.Threshold,
			Quota:         cr.Spec.TokenBudget.Quota,
			WindowSeconds: cr.Spec.TokenBudget.WindowSeconds,
		},
		MLClassifier: MLClassifierConfig{
			Enabled:     cr.Spec.MLClassifier.Enabled,
			ModelPath:   cr.Spec.MLClassifier.ModelPath,
			MappingPath: cr.Spec.MLClassifier.MappingPath,
			NumClasses:  cr.Spec.MLClassifier.NumClasses,
			UseCPU:      cr.Spec.MLClassifier.UseCPU,
			Threshold:   cr.Spec.MLClassifier.Threshold,
		},
	}
}

// ── Legacy combined config.yaml ────────────────────────────────────────────────

type legacyCombined struct {
	Models       map[string]legacyModel `yaml:"models"`
	Weights      legacyWeights          `yaml:"weights"`
	KeywordRules []KeywordRule          `yaml:"keyword_rules"`
	DefaultModel string                 `yaml:"default_model"`
	TokenBudget  legacyTokenBudget      `yaml:"token_budget"`
	MLClassifier MLClassifierConfig     `yaml:"ml_classifier"`
}

type legacyModel struct {
	Cost             float64  `yaml:"cost"`
	Domains          []string `yaml:"domains"`
	AverageLatencyMs float64  `yaml:"average_latency_ms"`
}

type legacyWeights struct {
	Domain      float64 `yaml:"domain"`
	Latency     float64 `yaml:"latency"`
	Cost        float64 `yaml:"cost"`
	TokenBudget float64 `yaml:"token_budget"`
}

type legacyTokenBudget struct {
	Threshold     int64 `yaml:"threshold"`
	Quota         int64 `yaml:"quota"`
	WindowSeconds int64 `yaml:"window_seconds"`
}

// LoadConfig reads a legacy combined config.yaml and converts it to RouterConfig.
// Domain lists become quality scores of 1.0 per domain.
func LoadConfig(path string) (*RouterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	lc := &legacyCombined{}
	if err := yaml.Unmarshal(data, lc); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	if lc.DefaultModel == "" {
		lc.DefaultModel = "default"
	}
	models := make(map[string]ModelConfig, len(lc.Models))
	for name, m := range lc.Models {
		qs := make(map[string]float64, len(m.Domains))
		for _, d := range m.Domains {
			qs[d] = 1.0
		}
		models[name] = ModelConfig{
			QualityScores:           qs,
			CostScore:               m.Cost,
			InitialAverageLatencyMs: m.AverageLatencyMs,
		}
	}
	return &RouterConfig{
		Models: models,
		Weights: ScoringWeights{
			Quality: lc.Weights.Domain,
			Latency: lc.Weights.Latency,
			Cost:    lc.Weights.Cost,
		},
		KeywordRules: lc.KeywordRules,
		DefaultModel: lc.DefaultModel,
		TokenBudget: TokenBudgetConfig{
			Enabled:       true,
			Threshold:     lc.TokenBudget.Threshold,
			Quota:         lc.TokenBudget.Quota,
			WindowSeconds: lc.TokenBudget.WindowSeconds,
		},
		MLClassifier: lc.MLClassifier,
	}, nil
}
