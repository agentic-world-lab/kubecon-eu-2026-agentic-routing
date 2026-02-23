package decision

import (
	"sort"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// ModelMetadata holds the static configuration for a model
type ModelMetadata struct {
	Cost             float64  `yaml:"cost"`
	Domains          []string `yaml:"domains"`
	AverageLatencyMs float64  `yaml:"average_latency_ms"`
}

// ScoringWeights defines the weights for the scoring formula
type ScoringWeights struct {
	Domain  float64 `yaml:"domain"`
	Latency float64 `yaml:"latency"`
	Cost    float64 `yaml:"cost"`
}

// ModelScore represents the computed score for a model
type ModelScore struct {
	ModelName    string  `json:"model_name"`
	DomainScore  float64 `json:"domain_score"`
	LatencyScore float64 `json:"latency_score"`
	CostScore    float64 `json:"cost_score"`
	FinalScore   float64 `json:"final_score"`
}

// ScoreModels evaluates all models and returns them ranked by final score.
// Formula: final_score = (w_domain * domain_score) + (w_latency * latency_score) + (w_cost * cost_score)
func ScoreModels(detectedDomain string, models map[string]ModelMetadata, weights ScoringWeights) []ModelScore {
	if len(models) == 0 {
		return nil
	}

	// Find max latency and max cost for normalization
	var maxLatency, maxCost float64
	for _, m := range models {
		if m.AverageLatencyMs > maxLatency {
			maxLatency = m.AverageLatencyMs
		}
		if m.Cost > maxCost {
			maxCost = m.Cost
		}
	}

	var scores []ModelScore

	for name, m := range models {
		// Domain score: 1.0 if model serves detected domain, 0.0 otherwise
		domainScore := 0.0
		for _, d := range m.Domains {
			if d == detectedDomain {
				domainScore = 1.0
				break
			}
		}

		// Latency score: normalized inverse (lower latency = higher score)
		latencyScore := 0.0
		if maxLatency > 0 {
			latencyScore = 1.0 - (m.AverageLatencyMs / maxLatency)
		}

		// Cost score: normalized inverse (lower cost = higher score)
		costScore := 0.0
		if maxCost > 0 {
			costScore = 1.0 - (m.Cost / maxCost)
		}

		finalScore := (weights.Domain * domainScore) +
			(weights.Latency * latencyScore) +
			(weights.Cost * costScore)

		scores = append(scores, ModelScore{
			ModelName:    name,
			DomainScore:  domainScore,
			LatencyScore: latencyScore,
			CostScore:    costScore,
			FinalScore:   finalScore,
		})

		logging.Infof("[Scorer] Model %s: domain=%.2f, latency=%.2f, cost=%.2f, final=%.4f",
			name, domainScore, latencyScore, costScore, finalScore)
	}

	// Sort by final score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].FinalScore > scores[j].FinalScore
	})

	if len(scores) > 0 {
		logging.Infof("[Scorer] Best model: %s (score: %.4f) for domain: %s",
			scores[0].ModelName, scores[0].FinalScore, detectedDomain)
	}

	return scores
}
