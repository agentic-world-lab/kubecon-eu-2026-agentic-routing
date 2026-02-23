package extproc

import (
	"fmt"

	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/classification"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/config"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// OpenAIRouter is an Envoy ExtProc server that routes OpenAI API requests
// based on domain classification and weighted scoring (domain + latency + cost).
type OpenAIRouter struct {
	Config     *config.RouterConfig
	Classifier *classification.Classifier
}

// Ensure OpenAIRouter implements the ext_proc interface
var _ ext_proc.ExternalProcessorServer = (*OpenAIRouter)(nil)

// NewOpenAIRouter creates a new router instance
func NewOpenAIRouter(configPath string) (*OpenAIRouter, error) {
	cfg, err := config.Parse(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	config.Replace(cfg)

	// Load category mapping if configured
	var categoryMapping *classification.CategoryMapping
	if cfg.Classifier.CategoryModel.CategoryMappingPath != "" {
		categoryMapping, err = classification.LoadCategoryMapping(cfg.Classifier.CategoryModel.CategoryMappingPath)
		if err != nil {
			logging.Warnf("Failed to load category mapping: %v", err)
		} else {
			logging.Infof("Loaded category mapping with %d categories", categoryMapping.GetCategoryCount())
		}
	}

	// Create classifier for domain classification
	classifier, err := classification.NewClassifier(cfg, categoryMapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create classifier: %w", err)
	}

	logging.Infof("Model Router initialized with %d models and %d categories",
		len(cfg.Models), len(cfg.Categories))

	return &OpenAIRouter{
		Config:     cfg,
		Classifier: classifier,
	}, nil
}
