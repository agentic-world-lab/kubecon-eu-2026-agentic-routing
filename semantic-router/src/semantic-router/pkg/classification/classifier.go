package classification

import (
	"fmt"

	candle_binding "github.com/vllm-project/semantic-router/candle-binding"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/config"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// CategoryInitializer initializes a category classification model
type CategoryInitializer interface {
	Init(modelID string, useCPU bool, numClasses ...int) error
}

// CategoryInitializerImpl auto-detects and initializes category classifier
type CategoryInitializerImpl struct {
	usedModernBERT bool
}

func (c *CategoryInitializerImpl) Init(modelID string, useCPU bool, numClasses ...int) error {
	success := candle_binding.InitCandleBertClassifier(modelID, numClasses[0], useCPU)
	if success {
		c.usedModernBERT = false
		logging.Infof("Initialized category classifier with auto-detection")
		return nil
	}

	logging.Infof("Auto-detection failed, falling back to ModernBERT category initializer")
	err := candle_binding.InitModernBertClassifier(modelID, useCPU)
	if err != nil {
		return fmt.Errorf("failed to initialize category classifier: %w", err)
	}
	c.usedModernBERT = true
	logging.Infof("Initialized ModernBERT category classifier (fallback mode)")
	return nil
}

// CategoryInference performs category classification inference
type CategoryInference interface {
	Classify(text string) (candle_binding.ClassResult, error)
}

// CategoryInferenceImpl auto-detects and performs category inference
type CategoryInferenceImpl struct{}

func (c *CategoryInferenceImpl) Classify(text string) (candle_binding.ClassResult, error) {
	result, err := candle_binding.ClassifyCandleBertText(text)
	if err != nil {
		return candle_binding.ClassifyModernBertText(text)
	}
	return result, nil
}

// Classifier handles domain classification for routing decisions
type Classifier struct {
	categoryInitializer         CategoryInitializer
	categoryInference           CategoryInference
	keywordClassifier           *KeywordClassifier
	keywordEmbeddingInitializer EmbeddingClassifierInitializer
	keywordEmbeddingClassifier  *EmbeddingClassifier

	Config          *config.RouterConfig
	CategoryMapping *CategoryMapping

	// Category name mapping
	MMLUToGeneric map[string]string
	GenericToMMLU map[string][]string
}

// NewClassifier creates a new classifier for domain classification only.
func NewClassifier(cfg *config.RouterConfig, categoryMapping *CategoryMapping) (*Classifier, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	classifier := &Classifier{
		Config:          cfg,
		CategoryMapping: categoryMapping,
	}

	// Set up category initializer and inference
	if cfg.Classifier.CategoryModel.ModelID != "" {
		classifier.categoryInitializer = &CategoryInitializerImpl{}
		classifier.categoryInference = &CategoryInferenceImpl{}
	}

	// Set up keyword classifier
	if len(cfg.KeywordRules) > 0 {
		kc, err := NewKeywordClassifier(cfg.KeywordRules)
		if err != nil {
			return nil, fmt.Errorf("failed to create keyword classifier: %w", err)
		}
		classifier.keywordClassifier = kc
	}

	// Set up embedding classifier
	if len(cfg.EmbeddingRules) > 0 {
		ec, err := NewEmbeddingClassifier(cfg.EmbeddingRules, cfg.EmbeddingModels.HNSWConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create embedding classifier: %w", err)
		}
		classifier.keywordEmbeddingInitializer = createEmbeddingInitializer()
		classifier.keywordEmbeddingClassifier = ec
	}

	// Build category name mappings
	classifier.buildCategoryNameMappings()

	// Initialize models
	return classifier.initModels()
}

// initModels initializes the ML models
func (c *Classifier) initModels() (*Classifier, error) {
	if c.IsCategoryEnabled() {
		if err := c.initializeCategoryClassifier(); err != nil {
			return nil, err
		}
	}

	if c.IsKeywordEmbeddingClassifierEnabled() {
		if err := c.initializeKeywordEmbeddingClassifier(); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// IsCategoryEnabled returns true if category classification is enabled
func (c *Classifier) IsCategoryEnabled() bool {
	return c.categoryInitializer != nil && c.Config.Classifier.CategoryModel.ModelID != ""
}

// IsKeywordEmbeddingClassifierEnabled returns true if embedding classifier is enabled
func (c *Classifier) IsKeywordEmbeddingClassifierEnabled() bool {
	return c.keywordEmbeddingClassifier != nil
}

// initializeCategoryClassifier initializes the category classification model
func (c *Classifier) initializeCategoryClassifier() error {
	numClasses := 0
	if c.CategoryMapping != nil {
		numClasses = c.CategoryMapping.GetCategoryCount()
	}

	if err := c.categoryInitializer.Init(c.Config.Classifier.CategoryModel.ModelID, c.Config.Classifier.CategoryModel.UseCPU, numClasses); err != nil {
		return fmt.Errorf("failed to initialize category classifier: %w", err)
	}
	return nil
}

// initializeKeywordEmbeddingClassifier initializes the embedding classifier
func (c *Classifier) initializeKeywordEmbeddingClassifier() error {
	if c.keywordEmbeddingInitializer != nil {
		if err := c.keywordEmbeddingInitializer.Init(
			c.Config.EmbeddingModels.Qwen3ModelPath,
			c.Config.EmbeddingModels.GemmaModelPath,
			"", // no mmbert
			c.Config.EmbeddingModels.UseCPU,
		); err != nil {
			return fmt.Errorf("failed to initialize embedding classifier: %w", err)
		}
	}
	return nil
}

// ClassifyDomain classifies text into a domain category.
// Returns the detected domain name.
func (c *Classifier) ClassifyDomain(text string) (string, float64, error) {
	// Try keyword classification first
	if c.keywordClassifier != nil {
		category, confidence, err := c.keywordClassifier.Classify(text)
		if err == nil && category != "" {
			logging.Infof("Keyword classification matched: %s (confidence: %.2f)", category, confidence)
			return c.translateToGenericCategory(category), confidence, nil
		}
	}

	// Try embedding classification
	if c.keywordEmbeddingClassifier != nil {
		category, confidence, err := c.keywordEmbeddingClassifier.Classify(text)
		if err == nil && category != "" {
			logging.Infof("Embedding classification matched: %s (confidence: %.2f)", category, confidence)
			return c.translateToGenericCategory(category), confidence, nil
		}
	}

	// Try BERT-based category classification
	if c.categoryInference != nil {
		result, err := c.categoryInference.Classify(text)
		if err == nil {
			categoryName := fmt.Sprintf("class_%d", result.Class)
			if c.CategoryMapping != nil {
				if name, ok := c.CategoryMapping.GetCategoryFromIndex(result.Class); ok {
					categoryName = name
				}
			}
			logging.Infof("Category classification: %s (confidence: %.2f)", categoryName, result.Confidence)
			return c.translateToGenericCategory(categoryName), float64(result.Confidence), nil
		}
		logging.Warnf("Category classification failed: %v", err)
	}

	return "other", 0.0, nil
}

// translateToGenericCategory translates an MMLU category to a generic one
func (c *Classifier) translateToGenericCategory(category string) string {
	if c.MMLUToGeneric != nil {
		if generic, ok := c.MMLUToGeneric[category]; ok {
			return generic
		}
	}
	return category
}

// buildCategoryNameMappings builds MMLU <-> generic category name mappings
func (c *Classifier) buildCategoryNameMappings() {
	c.MMLUToGeneric = make(map[string]string)
	c.GenericToMMLU = make(map[string][]string)

	for _, cat := range c.Config.Categories {
		for _, mmluCat := range cat.MMLUCategories {
			c.MMLUToGeneric[mmluCat] = cat.Name
			c.GenericToMMLU[cat.Name] = append(c.GenericToMMLU[cat.Name], mmluCat)
		}
		// Also map the category name to itself
		c.MMLUToGeneric[cat.Name] = cat.Name
	}
}
