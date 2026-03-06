package main

import "sort"

// ModelScore is the computed score for a single model.
type ModelScore struct {
	ModelName    string  `json:"model_name"`
	QualityScore float64 `json:"quality_score"`
	LatencyScore float64 `json:"latency_score"`
	CostScore    float64 `json:"cost_score"`
	FinalScore   float64 `json:"final_score"`
}

// ScoreModels scores every model using a weighted normalised utility function
// and returns them ranked highest-first.
//
// Formula:
//
//	final = w_quality * Q_norm_i  -  w_latency * L_norm_i  -  w_cost_eff * C_norm_i
//
// where each dimension is min-max normalised to [0,1] across all models:
//
//	Q_norm_i = (Q_i - Q_min) / (Q_max - Q_min)   higher quality  → higher score
//	L_norm_i = (L_i - L_min) / (L_max - L_min)   higher latency  → penalised
//	C_norm_i = (C_i - C_min) / (C_max - C_min)   higher cost     → penalised
//
// When all values in a dimension are equal the normalised score is 0.5
// (neutral – preserves relative ordering from other dimensions).
//
// Q_i comes from models[i].QualityScores[detectedDomain] (0.0 if domain absent).
// L_i comes from latencyMs[i] (current EMA tracked per model).
// C_i comes from models[i].CostScore.
//
// Budget pressure (tokenBudgetEnabled && budgetPressure > 0) amplifies the cost
// weight to shift routing toward cheaper models as the token quota fills up:
//
//	effective_w_cost = w_cost * (1 + budgetPressure)
func ScoreModels(
	detectedDomain string,
	models map[string]ModelConfig,
	latencyMs map[string]float64,
	weights ScoringWeights,
	budgetPressure float64,
	tokenBudgetEnabled bool,
) []ModelScore {
	if len(models) == 0 {
		return nil
	}

	type rawEntry struct {
		name    string
		quality float64
		latency float64
		cost    float64
	}
	raws := make([]rawEntry, 0, len(models))
	for name, m := range models {
		q := m.QualityScores[detectedDomain] // 0.0 when domain not in map
		l := latencyMs[name]
		if l == 0 {
			l = m.InitialAverageLatencyMs
		}
		c := m.CostScore
		raws = append(raws, rawEntry{name: name, quality: q, latency: l, cost: c})
	}

	// Compute min/max per dimension.
	qMin, qMax := raws[0].quality, raws[0].quality
	lMin, lMax := raws[0].latency, raws[0].latency
	cMin, cMax := raws[0].cost, raws[0].cost
	for _, r := range raws[1:] {
		if r.quality < qMin {
			qMin = r.quality
		}
		if r.quality > qMax {
			qMax = r.quality
		}
		if r.latency < lMin {
			lMin = r.latency
		}
		if r.latency > lMax {
			lMax = r.latency
		}
		if r.cost < cMin {
			cMin = r.cost
		}
		if r.cost > cMax {
			cMax = r.cost
		}
	}

	// Effective cost weight – amplified under budget pressure.
	wCost := weights.Cost
	if tokenBudgetEnabled && budgetPressure > 0 {
		wCost = weights.Cost * (1 + budgetPressure)
	}

	norm := func(v, min, max float64) float64 {
		if max == min {
			return 0.5 // all equal: neutral
		}
		return (v - min) / (max - min)
	}

	scores := make([]ModelScore, 0, len(raws))
	for _, r := range raws {
		qNorm := norm(r.quality, qMin, qMax)
		lNorm := norm(r.latency, lMin, lMax)
		cNorm := norm(r.cost, cMin, cMax)
		final := weights.Quality*qNorm - weights.Latency*lNorm - wCost*cNorm
		scores = append(scores, ModelScore{
			ModelName:    r.name,
			QualityScore: qNorm,
			LatencyScore: lNorm,
			CostScore:    cNorm,
			FinalScore:   final,
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].FinalScore > scores[j].FinalScore
	})
	return scores
}
