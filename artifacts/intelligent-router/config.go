package main

import (
	"math"
	"os"
	"strconv"
)

// ── Shared runtime types ───────────────────────────────────────────────────────

// RouterConfig is the unified routing configuration used at runtime.
// It is built dynamically from LLMBackend CRs discovered via the watcher.
type RouterConfig struct {
	Models       map[string]ModelConfig
	Weights      ScoringWeights
	KeywordRules []KeywordRule
	DefaultModel string
	TokenBudget  TokenBudgetConfig
	MLClassifier MLClassifierConfig
}

// ModelConfig is the runtime metadata for one backend model.
type ModelConfig struct {
	// QualityScores maps domain name to quality score in [0,1].
	QualityScores map[string]float64
	// CostScore is a normalised cost value in [0,1]. Higher = more expensive.
	CostScore float64
	// InitialAverageLatencyMs seeds the latency tracker for this model.
	InitialAverageLatencyMs float64
}

// ScoringWeights holds the weight for each scoring dimension.
type ScoringWeights struct {
	Quality float64
	Latency float64
	Cost    float64
}

// TokenBudgetConfig configures per-API-key token tracking and routing pressure.
type TokenBudgetConfig struct {
	Enabled       bool
	Threshold     int64
	Quota         int64
	WindowSeconds int64
}

// KeywordRule defines a rule for keyword-based domain classification.
type KeywordRule struct {
	Name          string
	Keywords      []string
	Operator      string
	CaseSensitive bool
}

// MLClassifierConfig configures the ML domain classifier.
type MLClassifierConfig struct {
	Enabled     bool
	ModelPath   string
	MappingPath string
	NumClasses  int
	UseCPU      bool
	Threshold   float64
}

// ── LLMBackend → RouterConfig conversion ────────────────────────────────────

// LLMBackendData is the intermediate representation of an evaluated LLMBackend CR.
type LLMBackendData struct {
	Name             string
	CategoryAccuracy map[string]float64 // MMLU-Pro category → accuracy [0,1]
	AvgResponseTime  float64            // seconds
	TokensPerSecond  float64
	PromptCost       float64 // per 1M tokens
	CompletionCost   float64 // per 1M tokens
}

// mmlpCategoryToDomain maps MMLU-Pro evaluation categories to router domains.
var mmluCategoryToDomain = map[string]string{
	"biology":          "science",
	"business":         "finance",
	"chemistry":        "science",
	"computer science": "technology",
	"economics":        "finance",
	"engineering":      "technology",
	"health":           "health",
	"history":          "legal",
	"law":              "legal",
	"math":             "science",
	"other":            "general",
	"philosophy":       "general",
	"physics":          "science",
	"psychology":       "health",
}

// weightsForTarget returns ScoringWeights based on the OPTIMIZATION_TARGET value.
func weightsForTarget(target string) ScoringWeights {
	switch target {
	case "latency":
		return ScoringWeights{Quality: 0.1, Latency: 0.8, Cost: 0.1}
	case "cost":
		return ScoringWeights{Quality: 0.1, Latency: 0.1, Cost: 0.8}
	default: // "accuracy" or unset
		return ScoringWeights{Quality: 0.8, Latency: 0.1, Cost: 0.1}
	}
}

// defaultKeywordRules returns the hardcoded keyword rules for domain classification.
func defaultKeywordRules() []KeywordRule {
	return []KeywordRule{
		{Name: "finance", Operator: "OR", Keywords: []string{
			"stock", "investment", "portfolio", "dividend", "trading", "bond", "equity", "fund",
		}},
		{Name: "legal", Operator: "OR", Keywords: []string{
			"law", "court", "contract", "attorney", "litigation", "statute", "lawsuit", "jurisdiction",
		}},
		{Name: "health", Operator: "OR", Keywords: []string{
			"health", "medical", "doctor", "disease", "treatment", "symptom", "hospital", "medicine", "patient",
		}},
		{Name: "technology", Operator: "OR", Keywords: []string{
			"software", "code", "programming", "algorithm", "database", "framework", "cloud", "kubernetes", "docker",
		}},
		{Name: "science", Operator: "OR", Keywords: []string{
			"physics", "chemistry", "biology", "experiment", "molecule", "quantum", "hypothesis", "laboratory",
		}},
	}
}

// defaultMLClassifierConfig returns the ML classifier config from env vars with defaults.
func defaultMLClassifierConfig() MLClassifierConfig {
	modelPath := envOrDefault("ML_MODEL_PATH", "/app/models/domain-classifier")
	mappingPath := envOrDefault("ML_MAPPING_PATH", "/app/models/domain-classifier/category_mapping.json")
	numClasses, _ := strconv.Atoi(envOrDefault("ML_NUM_CLASSES", "14"))
	threshold, _ := strconv.ParseFloat(envOrDefault("ML_THRESHOLD", "0.4"), 64)
	useCPU := envOrDefault("ML_USE_CPU", "true") == "true"

	return MLClassifierConfig{
		Enabled:     true,
		ModelPath:   modelPath,
		MappingPath: mappingPath,
		NumClasses:  numClasses,
		UseCPU:      useCPU,
		Threshold:   threshold,
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// convertLLMBackendsToConfig builds a RouterConfig from a slice of evaluated LLMBackend data.
func convertLLMBackendsToConfig(backends []LLMBackendData, optimizationTarget string) *RouterConfig {
	models := make(map[string]ModelConfig, len(backends))

	// First pass: compute cost scores.
	// Use pricing if available, otherwise use inverse throughput (1/tokensPerSecond)
	// as a proxy — higher throughput ≈ lower cost per token.
	type backendCost struct {
		rawCost float64
	}
	costs := make([]backendCost, len(backends))
	var minCost, maxCost float64
	minCost = math.MaxFloat64
	for i, b := range backends {
		var raw float64
		if b.PromptCost+b.CompletionCost > 0 {
			raw = b.PromptCost + b.CompletionCost
		} else if b.TokensPerSecond > 0 {
			raw = 1.0 / b.TokensPerSecond // inverse throughput as cost proxy
		}
		costs[i] = backendCost{rawCost: raw}
		if raw < minCost {
			minCost = raw
		}
		if raw > maxCost {
			maxCost = raw
		}
	}
	costRange := maxCost - minCost
	if costRange == 0 {
		costRange = 1
	}

	defaultModel := ""
	cheapest := math.MaxFloat64

	for i, b := range backends {
		// Aggregate categoryAccuracy → domain quality scores.
		domainSums := make(map[string]float64)
		domainCounts := make(map[string]int)
		for category, accuracy := range b.CategoryAccuracy {
			domain, ok := mmluCategoryToDomain[category]
			if !ok {
				domain = "general"
			}
			domainSums[domain] += accuracy
			domainCounts[domain]++
		}
		qualityScores := make(map[string]float64, len(domainSums))
		for domain, sum := range domainSums {
			qualityScores[domain] = sum / float64(domainCounts[domain])
		}

		costScore := (costs[i].rawCost - minCost) / costRange

		models[b.Name] = ModelConfig{
			QualityScores:           qualityScores,
			CostScore:               costScore,
			InitialAverageLatencyMs: b.AvgResponseTime * 1000, // seconds → ms
		}

		if costs[i].rawCost < cheapest {
			cheapest = costs[i].rawCost
			defaultModel = b.Name
		}
	}

	return &RouterConfig{
		Models:       models,
		Weights:      weightsForTarget(optimizationTarget),
		KeywordRules: defaultKeywordRules(),
		DefaultModel: defaultModel,
		TokenBudget: TokenBudgetConfig{
			Enabled:       true,
			Threshold:     500,
			Quota:         1000,
			WindowSeconds: 60,
		},
		MLClassifier: defaultMLClassifierConfig(),
	}
}
