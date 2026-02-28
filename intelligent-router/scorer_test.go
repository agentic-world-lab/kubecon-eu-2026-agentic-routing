package main

import (
	"testing"
)

// latencyMapFrom builds a latency map from model InitialAverageLatencyMs values.
func latencyMapFrom(models map[string]ModelConfig) map[string]float64 {
	m := make(map[string]float64, len(models))
	for name, mc := range models {
		m[name] = mc.InitialAverageLatencyMs
	}
	return m
}

// twoModels returns a simple model pair used across several scorer tests.
//   modelA: high quality for finance, higher cost, lower latency
//   modelB: no finance quality, lower cost, higher latency
func twoModels() map[string]ModelConfig {
	return map[string]ModelConfig{
		"model-a": {
			QualityScores:           map[string]float64{"finance": 1.0, "legal": 0.8},
			CostScore:               0.8,
			InitialAverageLatencyMs: 100,
		},
		"model-b": {
			QualityScores:           map[string]float64{"technology": 1.0},
			CostScore:               0.2,
			InitialAverageLatencyMs: 200,
		},
	}
}

func TestScoreModels_QualityMatch(t *testing.T) {
	models := twoModels()
	weights := ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2}
	latency := latencyMapFrom(models)

	scores := ScoreModels("finance", models, latency, weights, 0.0, false)

	if len(scores) == 0 {
		t.Fatal("expected scores, got none")
	}
	if scores[0].ModelName != "model-a" {
		t.Errorf("expected model-a to win finance quality match, got %s", scores[0].ModelName)
	}
	// model-a has normalised quality 1.0 for finance (model-b has 0.0)
	if scores[0].QualityScore != 1.0 {
		t.Errorf("expected quality score 1.0, got %f", scores[0].QualityScore)
	}
}

func TestScoreModels_NoDomainMatch_AllQualityNeutral(t *testing.T) {
	models := twoModels()
	weights := ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2}
	latency := latencyMapFrom(models)

	scores := ScoreModels("unknown", models, latency, weights, 0.0, false)

	// Both models return 0.0 for unknown domain → normalised quality is 0.5 for both.
	for _, s := range scores {
		if abs(s.QualityScore-0.5) > 1e-9 {
			t.Errorf("%s: expected neutral quality score 0.5 for unknown domain, got %f", s.ModelName, s.QualityScore)
		}
	}
}

func TestScoreModels_LatencyNormalization(t *testing.T) {
	models := map[string]ModelConfig{
		"fast": {QualityScores: map[string]float64{}, CostScore: 0.5, InitialAverageLatencyMs: 50},
		"slow": {QualityScores: map[string]float64{}, CostScore: 0.5, InitialAverageLatencyMs: 200},
	}
	weights := ScoringWeights{Quality: 0.0, Latency: 1.0, Cost: 0.0}
	latency := latencyMapFrom(models)

	scores := ScoreModels("other", models, latency, weights, 0.0, false)

	if len(scores) == 0 {
		t.Fatal("expected scores, got none")
	}
	if scores[0].ModelName != "fast" {
		t.Errorf("expected fast model to win latency contest, got %s", scores[0].ModelName)
	}
	for _, s := range scores {
		switch s.ModelName {
		case "fast":
			if abs(s.LatencyScore-0.0) > 1e-9 {
				t.Errorf("fast: latency_score want 0.0 (min), got %f", s.LatencyScore)
			}
		case "slow":
			if abs(s.LatencyScore-1.0) > 1e-9 {
				t.Errorf("slow: latency_score want 1.0 (max), got %f", s.LatencyScore)
			}
		}
	}
}

func TestScoreModels_CostNormalization(t *testing.T) {
	models := map[string]ModelConfig{
		"cheap":     {QualityScores: map[string]float64{}, CostScore: 0.1, InitialAverageLatencyMs: 100},
		"expensive": {QualityScores: map[string]float64{}, CostScore: 0.9, InitialAverageLatencyMs: 100},
	}
	weights := ScoringWeights{Quality: 0.0, Latency: 0.0, Cost: 1.0}
	latency := latencyMapFrom(models)

	scores := ScoreModels("other", models, latency, weights, 0.0, false)

	if len(scores) == 0 {
		t.Fatal("expected scores, got none")
	}
	if scores[0].ModelName != "cheap" {
		t.Errorf("expected cheap model to win cost contest, got %s", scores[0].ModelName)
	}
	// cheap has cost_score 0.0 (min) and expensive has 1.0 (max)
	for _, s := range scores {
		switch s.ModelName {
		case "cheap":
			if abs(s.CostScore-0.0) > 1e-9 {
				t.Errorf("cheap: cost_score want 0.0, got %f", s.CostScore)
			}
		case "expensive":
			if abs(s.CostScore-1.0) > 1e-9 {
				t.Errorf("expensive: cost_score want 1.0, got %f", s.CostScore)
			}
		}
	}
}

func TestScoreModels_Empty(t *testing.T) {
	if scores := ScoreModels("finance", nil, nil, ScoringWeights{}, 0.0, false); len(scores) != 0 {
		t.Errorf("expected empty scores for nil models, got %d", len(scores))
	}
}

func TestScoreModels_SortedDescending(t *testing.T) {
	models := twoModels()
	weights := ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2}
	latency := latencyMapFrom(models)

	scores := ScoreModels("finance", models, latency, weights, 0.0, false)

	for i := 1; i < len(scores); i++ {
		if scores[i].FinalScore > scores[i-1].FinalScore {
			t.Errorf("scores not sorted descending at index %d", i)
		}
	}
}

func TestScoreModels_EqualValues_NeutralScore(t *testing.T) {
	// Single model: all dimensions equal → normalised to 0.5 each.
	models := map[string]ModelConfig{
		"m": {QualityScores: map[string]float64{"finance": 0.5}, CostScore: 0.5, InitialAverageLatencyMs: 100},
	}
	weights := ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2}
	latency := latencyMapFrom(models)

	scores := ScoreModels("finance", models, latency, weights, 0.0, false)
	if len(scores) == 0 {
		t.Fatal("expected one score")
	}
	// final = 0.5*0.5 - 0.3*0.5 - 0.2*0.5 = 0.25 - 0.15 - 0.10 = 0.0
	if abs(scores[0].FinalScore-0.0) > 1e-9 {
		t.Errorf("single model: want final 0.0, got %.4f", scores[0].FinalScore)
	}
}

func TestScoreModels_BudgetPressure_NoPressure_SameAsDisabled(t *testing.T) {
	models := twoModels()
	weights := ScoringWeights{Quality: 0.5, Latency: 0.3, Cost: 0.2}
	latency := latencyMapFrom(models)

	enabled := ScoreModels("finance", models, latency, weights, 0.0, true)
	disabled := ScoreModels("finance", models, latency, weights, 0.0, false)

	for i := range enabled {
		if abs(enabled[i].FinalScore-disabled[i].FinalScore) > 1e-9 {
			t.Errorf("at pressure=0, enabled and disabled should give same score; %s: %f vs %f",
				enabled[i].ModelName, enabled[i].FinalScore, disabled[i].FinalScore)
		}
	}
}

func TestScoreModels_BudgetPressure_FullPressure_BoostsCheap(t *testing.T) {
	// expensive: high quality for finance, high cost
	// cheap: no quality for finance, low cost, same latency
	models := map[string]ModelConfig{
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
	}
	// Quality=0.5, Latency=0.1, Cost=0.3: at pressure=0 expensive wins;
	// at pressure=1.0 effective w_cost=0.6 and cheap wins.
	weights := ScoringWeights{Quality: 0.5, Latency: 0.1, Cost: 0.3}
	latency := latencyMapFrom(models)

	// Verify expensive wins with no pressure.
	at0 := ScoreModels("finance", models, latency, weights, 0.0, true)
	if at0[0].ModelName != "expensive" {
		t.Errorf("at pressure=0 expensive should win, got %s", at0[0].ModelName)
	}

	// Verify cheap wins at full pressure.
	at1 := ScoreModels("finance", models, latency, weights, 1.0, true)
	if at1[0].ModelName != "cheap" {
		t.Errorf("at pressure=1.0 cheap should win, got %s", at1[0].ModelName)
	}
}

func TestScoreModels_BudgetPressure_DisabledIgnoresPressure(t *testing.T) {
	models := map[string]ModelConfig{
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
	}
	weights := ScoringWeights{Quality: 0.5, Latency: 0.1, Cost: 0.3}
	latency := latencyMapFrom(models)

	// With tokenBudgetEnabled=false, pressure 1.0 should be ignored.
	scores := ScoreModels("finance", models, latency, weights, 1.0, false)
	if scores[0].ModelName != "expensive" {
		t.Errorf("budget disabled: expensive should still win despite pressure=1.0, got %s", scores[0].ModelName)
	}
}

func TestScoreModels_TrackedLatencyOverridesInitial(t *testing.T) {
	models := map[string]ModelConfig{
		"model-a": {QualityScores: map[string]float64{}, CostScore: 0.5, InitialAverageLatencyMs: 100},
		"model-b": {QualityScores: map[string]float64{}, CostScore: 0.5, InitialAverageLatencyMs: 50},
	}
	weights := ScoringWeights{Quality: 0.0, Latency: 1.0, Cost: 0.0}

	// Override: model-a has been observed at 20ms (faster than model-b initial 50ms).
	latency := map[string]float64{"model-a": 20, "model-b": 50}

	scores := ScoreModels("other", models, latency, weights, 0.0, false)
	if scores[0].ModelName != "model-a" {
		t.Errorf("with tracked latency, model-a (20ms) should beat model-b (50ms), got %s", scores[0].ModelName)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
