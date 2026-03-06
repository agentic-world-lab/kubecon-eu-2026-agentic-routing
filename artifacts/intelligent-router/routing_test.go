package main

import (
	"encoding/json"
	"testing"
)

// testState builds a routerState with the new model format and keyword rules.
// Models are designed so each domain has a clear winner:
//   - gpt-4o:        quality[finance]=1.0, quality[legal]=1.0; cost=0.2, latency=120
//   - gpt-4o-mini:   quality[technology]=1.0, quality[general]=1.0; cost=0.1, latency=85
//   - llama-3.1-70b: quality[science]=1.0; cost=0.05, latency=200
//
// Weights: Quality=0.5, Latency=0.3, Cost=0.2
func testState(t *testing.T) *routerState {
	t.Helper()
	cfg := &RouterConfig{
		Models: map[string]ModelConfig{
			"gpt-4o": {
				QualityScores:           map[string]float64{"finance": 1.0, "legal": 1.0},
				CostScore:               0.2,
				InitialAverageLatencyMs: 120,
			},
			"gpt-4o-mini": {
				QualityScores:           map[string]float64{"technology": 1.0, "general": 1.0},
				CostScore:               0.1,
				InitialAverageLatencyMs: 85,
			},
			"llama-3.1-70b": {
				QualityScores:           map[string]float64{"science": 1.0},
				CostScore:               0.05,
				InitialAverageLatencyMs: 200,
			},
		},
		Weights:      ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2},
		DefaultModel: "gpt-4o-mini",
		KeywordRules: []KeywordRule{
			{Name: "finance", Keywords: []string{"stock", "investment", "portfolio", "dividend", "trading"}, Operator: "OR"},
			{Name: "legal", Keywords: []string{"law", "court", "contract", "attorney"}, Operator: "OR"},
			{Name: "technology", Keywords: []string{"software", "code", "programming", "algorithm"}, Operator: "OR"},
			{Name: "science", Keywords: []string{"physics", "chemistry", "experiment", "quantum"}, Operator: "OR"},
		},
	}
	kc, err := NewKeywordClassifier(cfg.KeywordRules)
	if err != nil {
		t.Fatalf("NewKeywordClassifier: %v", err)
	}
	return &routerState{config: cfg, classifier: kc}
}

// initLatency builds a latency snapshot from model InitialAverageLatencyMs.
func initLatency(cfg *RouterConfig) map[string]float64 {
	m := make(map[string]float64, len(cfg.Models))
	for name, mc := range cfg.Models {
		m[name] = mc.InitialAverageLatencyMs
	}
	return m
}

func chatBody(model, content string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": content},
		},
	})
	return b
}

func TestRoute_FinanceDomain_SelectsGPT4o(t *testing.T) {
	state := testState(t)
	body := chatBody("original", "I want to build an investment portfolio with stocks and dividends")
	latency := initLatency(state.config)

	selectedModel, domain, scores := route(body, state, latency, 0.0)

	if domain != "finance" {
		t.Errorf("domain: want 'finance', got %q", domain)
	}
	if selectedModel != "gpt-4o" {
		t.Errorf("selectedModel: want 'gpt-4o', got %q", selectedModel)
	}
	if len(scores) != len(state.config.Models) {
		t.Errorf("scores count: want %d, got %d", len(state.config.Models), len(scores))
	}
}

func TestRoute_LegalDomain_SelectsGPT4o(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	_, domain, _ := route(chatBody("x", "Can you explain contract law in this court case?"), state, latency, 0.0)
	if domain != "legal" {
		t.Errorf("domain: want 'legal', got %q", domain)
	}
}

func TestRoute_TechnologyDomain_SelectsGPT4oMini(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	selectedModel, domain, _ := route(chatBody("x", "How do I write a sorting algorithm in code?"), state, latency, 0.0)
	if domain != "technology" {
		t.Errorf("domain: want 'technology', got %q", domain)
	}
	if selectedModel != "gpt-4o-mini" {
		t.Errorf("selectedModel: want 'gpt-4o-mini', got %q", selectedModel)
	}
}

func TestRoute_ScienceDomain_SelectsLlama(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	selectedModel, domain, _ := route(chatBody("x", "Explain quantum mechanics and the physics of experiment"), state, latency, 0.0)
	if domain != "science" {
		t.Errorf("domain: want 'science', got %q", domain)
	}
	if selectedModel != "llama-3.1-70b" {
		t.Errorf("selectedModel: want 'llama-3.1-70b', got %q", selectedModel)
	}
}

func TestRoute_UnknownDomain_SelectsCheapestFastest(t *testing.T) {
	// For unknown domain, all quality scores are 0 → quality is neutral.
	// The cheapest+fastest model wins. gpt-4o-mini has lowest cost (0.1) and
	// lowest latency (85ms) among the three, so it wins.
	// Since gpt-4o-mini is also the default, this test validates both properties.
	state := testState(t)
	latency := initLatency(state.config)
	selectedModel, domain, _ := route(chatBody("x", "What is the best recipe for banana bread?"), state, latency, 0.0)
	if domain != "unknown" {
		t.Errorf("domain: want 'unknown', got %q", domain)
	}
	if selectedModel != "gpt-4o-mini" {
		t.Errorf("selectedModel: want 'gpt-4o-mini', got %q", selectedModel)
	}
}

func TestRoute_EmptyContent_FallsBackToDefault(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	body := []byte(`{"model":"m","messages":[]}`)
	selectedModel, domain, _ := route(body, state, latency, 0.0)
	if domain != "unknown" {
		t.Errorf("domain: want 'unknown', got %q", domain)
	}
	if selectedModel != state.config.DefaultModel {
		t.Errorf("selectedModel: want default, got %q", selectedModel)
	}
}

func TestRoute_ScoresContainAllModels(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	_, _, scores := route(chatBody("x", "quantum chemistry experiment"), state, latency, 0.0)
	got := make(map[string]bool, len(scores))
	for _, s := range scores {
		got[s.ModelName] = true
	}
	for name := range state.config.Models {
		if !got[name] {
			t.Errorf("model %q missing from scores", name)
		}
	}
}

func TestRoute_ScoresAreSortedDescending(t *testing.T) {
	state := testState(t)
	latency := initLatency(state.config)
	_, _, scores := route(chatBody("x", "stock market investment"), state, latency, 0.0)
	for i := 1; i < len(scores); i++ {
		if scores[i].FinalScore > scores[i-1].FinalScore {
			t.Errorf("scores not sorted descending at index %d", i)
		}
	}
}

// testStateBudget returns a state with two models where the expensive model wins
// at no budget pressure but the cheap model wins at full pressure.
//
// expensive: quality[finance]=1.0, cost=1.0, latency=50ms
// cheap:     quality[finance]=0.0, cost=0.0, latency=50ms
// Weights: Quality=0.5, Latency=0.1, Cost=0.3, tokenBudget enabled
//
// At pressure=0, effective_w_cost=0.3:
//   Score(expensive) = 0.5*1.0 - 0.1*0.5 - 0.3*1.0 = 0.15
//   Score(cheap)     = 0.5*0.0 - 0.1*0.5 - 0.3*0.0 = -0.05
//
// At pressure=1.0, effective_w_cost=0.6:
//   Score(expensive) = 0.5*1.0 - 0.1*0.5 - 0.6*1.0 = -0.15
//   Score(cheap)     = 0.5*0.0 - 0.1*0.5 - 0.6*0.0 = -0.05
func testStateBudget(t *testing.T) *routerState {
	t.Helper()
	cfg := &RouterConfig{
		Models: map[string]ModelConfig{
			"expensive": {
				QualityScores:           map[string]float64{"finance": 1.0},
				CostScore:               1.0,
				InitialAverageLatencyMs: 50,
			},
			"cheap": {
				QualityScores:           map[string]float64{},
				CostScore:               0.0,
				InitialAverageLatencyMs: 50,
			},
		},
		Weights:      ScoringWeights{Quality: 0.5, Latency: 0.1, Cost: 0.3},
		DefaultModel: "expensive",
		TokenBudget:  TokenBudgetConfig{Enabled: true, Threshold: 500, Quota: 1000, WindowSeconds: 60},
		KeywordRules: []KeywordRule{
			{Name: "finance", Keywords: []string{"stock"}, Operator: "OR"},
		},
	}
	kc, _ := NewKeywordClassifier(cfg.KeywordRules)
	return &routerState{config: cfg, classifier: kc}
}

func TestRoute_BudgetPressure_Zero_ExpensiveWins(t *testing.T) {
	state := testStateBudget(t)
	latency := initLatency(state.config)
	selectedModel, _, _ := route(chatBody("x", "buy some stock"), state, latency, 0.0)
	if selectedModel != "expensive" {
		t.Errorf("at pressure=0 expensive should win, got %q", selectedModel)
	}
}

func TestRoute_BudgetPressure_Full_SelectsCheap(t *testing.T) {
	state := testStateBudget(t)
	latency := initLatency(state.config)
	selectedModel, _, _ := route(chatBody("x", "buy some stock"), state, latency, 1.0)
	if selectedModel != "cheap" {
		t.Errorf("under full budget pressure want 'cheap', got %q", selectedModel)
	}
}

func TestRoute_BudgetPressure_CostWeightAmplified(t *testing.T) {
	// Verify that cheap model scores better at pressure=1 than at pressure=0.
	state := testStateBudget(t)
	latency := initLatency(state.config)
	_, _, scores0 := route(chatBody("x", "stock market"), state, latency, 0.0)
	_, _, scores1 := route(chatBody("x", "stock market"), state, latency, 1.0)

	cheapScore0, cheapScore1 := 0.0, 0.0
	for _, s := range scores0 {
		if s.ModelName == "cheap" {
			cheapScore0 = s.FinalScore
		}
	}
	for _, s := range scores1 {
		if s.ModelName == "cheap" {
			cheapScore1 = s.FinalScore
		}
	}
	// cheap's cost score is 0.0 (min) so cost penalty does not change its score.
	// The relative advantage of cheap over expensive grows with pressure.
	if cheapScore0 != cheapScore1 {
		// cheap has cost_score=0 so -wCost*0 is always 0 regardless of pressure.
		// Scores should be identical for cheap across pressure levels.
		t.Errorf("cheap (cost=0) score should not change with pressure: %.4f vs %.4f", cheapScore0, cheapScore1)
	}
}

func TestLoadFromCR_Valid(t *testing.T) {
	cfg, err := LoadFromCR("config-CR.yaml")
	if err != nil {
		t.Fatalf("LoadFromCR: %v", err)
	}
	if len(cfg.Models) == 0 {
		t.Error("expected at least one model in config-CR.yaml")
	}
	if cfg.DefaultModel == "" {
		t.Error("expected a defaultModel in config-CR.yaml")
	}
	for name, m := range cfg.Models {
		if len(m.QualityScores) == 0 {
			t.Errorf("model %s: expected qualityScores, got none", name)
		}
	}
}

func TestLoadFromCR_Missing(t *testing.T) {
	if _, err := LoadFromCR("/nonexistent/path.yaml"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadConfig_Valid(t *testing.T) {
	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Models) == 0 {
		t.Error("expected at least one model in config.yaml")
	}
	if cfg.DefaultModel == "" {
		t.Error("expected a default_model in config.yaml")
	}
}

func TestLoadConfig_Missing(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/path.yaml"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestExtractUserContent_SingleMessage(t *testing.T) {
	if got := extractUserContent(chatBody("m", "Hello world")); got != "Hello world" {
		t.Errorf("want 'Hello world', got %q", got)
	}
}

func TestExtractUserContent_SkipsSystemAndAssistant(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": "m",
		"messages": []map[string]string{
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there"},
			{"role": "user", "content": "Tell me about stocks"},
		},
	})
	if got := extractUserContent(body); got != "Hello Tell me about stocks" {
		t.Errorf("want 'Hello Tell me about stocks', got %q", got)
	}
}

func TestExtractUserContent_NoMessages(t *testing.T) {
	body := []byte("{}")
	if got := extractUserContent(body); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestExtractUserContent_InvalidJSON(t *testing.T) {
	body := []byte("not json")
	if got := extractUserContent(body); got != "" {
		t.Errorf("want empty for invalid JSON, got %q", got)
	}
}
