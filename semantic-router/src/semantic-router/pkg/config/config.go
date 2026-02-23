package config

// RouterConfig represents the main configuration for the Model Router Service.
// Stripped down to only domain classification + latency + cost scoring.
type RouterConfig struct {
	// Embedding models configuration
	EmbeddingModels EmbeddingModels `yaml:"embedding_models"`

	// Classifier configuration for domain classification
	Classifier ClassifierConfig `yaml:"classifier"`

	// Categories define domain metadata
	Categories []Category `yaml:"categories,omitempty"`

	// KeywordRules for keyword-based signal extraction
	KeywordRules []KeywordRule `yaml:"keyword_rules,omitempty"`

	// EmbeddingRules for embedding-based signal extraction
	EmbeddingRules []EmbeddingRule `yaml:"embedding_rules,omitempty"`

	// Models defines the available backend models with cost and latency
	Models map[string]ModelRouterModel `yaml:"models"`

	// Weights for the scoring formula
	Weights ScoringWeights `yaml:"weights"`

	// DefaultModel is used when no model scores high enough
	DefaultModel string `yaml:"default_model"`

	// Observability configuration
	Observability ObservabilityConfig `yaml:"observability"`
}

// ModelRouterModel defines a backend model with its metadata
type ModelRouterModel struct {
	Cost             float64  `yaml:"cost"`
	Domains          []string `yaml:"domains"`
	AverageLatencyMs float64  `yaml:"average_latency_ms"`
}

// ScoringWeights defines the weights for the weighted scoring formula
type ScoringWeights struct {
	Domain  float64 `yaml:"domain"`
	Latency float64 `yaml:"latency"`
	Cost    float64 `yaml:"cost"`
}

// EmbeddingModels configuration for Qwen3/Gemma embedding models
type EmbeddingModels struct {
	Qwen3ModelPath string `yaml:"qwen3_model_path"`
	GemmaModelPath string `yaml:"gemma_model_path"`
	UseCPU         bool   `yaml:"use_cpu"`
	HNSWConfig     HNSWConfig `yaml:"hnsw_config"`
}

// HNSWConfig for embedding classifier
type HNSWConfig struct {
	ModelType          string  `yaml:"model_type"`
	PreloadEmbeddings  bool    `yaml:"preload_embeddings"`
	TargetDimension    int     `yaml:"target_dimension"`
	EnableSoftMatching *bool   `yaml:"enable_soft_matching,omitempty"`
	MinScoreThreshold  float64 `yaml:"min_score_threshold"`
}

// WithDefaults returns a copy of HNSWConfig with defaults applied
func (c HNSWConfig) WithDefaults() HNSWConfig {
	if c.ModelType == "" {
		c.ModelType = "qwen3"
	}
	if c.TargetDimension == 0 {
		c.TargetDimension = 1024
	}
	if c.MinScoreThreshold == 0 {
		c.MinScoreThreshold = 0.5
	}
	return c
}

// AggregationMethod defines how to aggregate embedding similarities
type AggregationMethod string

const (
	AggregationMethodMean AggregationMethod = "mean"
	AggregationMethodMax  AggregationMethod = "max"
	AggregationMethodAny  AggregationMethod = "any"
)

// ResolveModelPath returns the path as-is (simplified - no registry lookup)
func ResolveModelPath(path string) string {
	return path
}

// ClassifierConfig for domain/category classification
type ClassifierConfig struct {
	CategoryModel CategoryModelConfig `yaml:"category_model"`
}

// CategoryModelConfig for the BERT-based category classifier
type CategoryModelConfig struct {
	ModelID             string  `yaml:"model_id"`
	Threshold           float64 `yaml:"threshold"`
	UseCPU              bool    `yaml:"use_cpu"`
	CategoryMappingPath string  `yaml:"category_mapping_path"`
}

// Category defines a domain category
type Category struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	MMLUCategories []string `yaml:"mmlu_categories,omitempty"`
}

// KeywordRule for keyword-based signal extraction
type KeywordRule struct {
	Name          string   `yaml:"name"`
	Keywords      []string `yaml:"keywords"`
	Operator      string   `yaml:"operator"`
	CaseSensitive bool     `yaml:"case_sensitive"`
}

// EmbeddingRule for embedding-based signal extraction
type EmbeddingRule struct {
	Name                     string            `yaml:"name"`
	Candidates               []string          `yaml:"candidates"`
	SimilarityThreshold      float32           `yaml:"similarity_threshold"`
	AggregationMethodConfiged AggregationMethod `yaml:"aggregation,omitempty"`
}

// ObservabilityConfig for metrics and tracing
type ObservabilityConfig struct {
	Metrics MetricsConfig `yaml:"metrics"`
	Tracing TracingConfig `yaml:"tracing"`
}

// MetricsConfig for Prometheus metrics
type MetricsConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

// TracingConfig for distributed tracing
type TracingConfig struct {
	Enabled  bool           `yaml:"enabled"`
	Provider string         `yaml:"provider,omitempty"`
	Exporter ExporterConfig `yaml:"exporter,omitempty"`
	Sampling SamplingConfig `yaml:"sampling,omitempty"`
	Resource ResourceConfig `yaml:"resource,omitempty"`
}

// ExporterConfig for tracing exporter
type ExporterConfig struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	Insecure bool   `yaml:"insecure"`
}

// SamplingConfig for tracing sampling
type SamplingConfig struct {
	Type string  `yaml:"type"`
	Rate float64 `yaml:"rate"`
}

// ResourceConfig for tracing resource attributes
type ResourceConfig struct {
	ServiceName           string `yaml:"service_name"`
	ServiceVersion        string `yaml:"service_version"`
	DeploymentEnvironment string `yaml:"deployment_environment"`
}

// BatchSizeRangeConfig for metrics batch size tracking
type BatchSizeRangeConfig struct {
	Min   int    `yaml:"min"`
	Max   int    `yaml:"max"`
	Label string `yaml:"label"`
}

// WindowedMetricsConfig for windowed metrics
type WindowedMetricsConfig struct {
	Enabled              bool                    `yaml:"enabled"`
	WindowSize           string                  `yaml:"window_size,omitempty"`
	BucketCount          int                     `yaml:"bucket_count,omitempty"`
	TimeWindows          []string                `yaml:"time_windows,omitempty"`
	UpdateInterval       string                  `yaml:"update_interval,omitempty"`
	MaxModels            int                     `yaml:"max_models,omitempty"`
	QueueDepthEstimation bool `yaml:"queue_depth_estimation,omitempty"`
}

// Decision represents a routing decision with rules and model references
type Decision struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description,omitempty"`
	Priority    int             `yaml:"priority"`
	Rules       RuleCombination `yaml:"rules"`
}

// RuleCombination represents a combination of conditions with an operator
type RuleCombination struct {
	Operator   string      `yaml:"operator"` // AND or OR
	Conditions []Condition `yaml:"conditions"`
}

// Condition represents a single condition in a rule
type Condition struct {
	Type string `yaml:"type"` // keyword, embedding, domain, etc.
	Name string `yaml:"name"`
}

// GetCategoryDescriptions returns a list of category descriptions
func (c *RouterConfig) GetCategoryDescriptions() []string {
	var descriptions []string
	for _, cat := range c.Categories {
		descriptions = append(descriptions, cat.Description)
	}
	return descriptions
}
