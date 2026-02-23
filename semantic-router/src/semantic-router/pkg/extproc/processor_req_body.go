package extproc

import (
	"encoding/json"
	"fmt"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/decision"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/headers"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// handleRequestBody processes the request body for model routing
func (r *OpenAIRouter) handleRequestBody(
	v *ext_proc.ProcessingRequest_RequestBody,
	ctx *RequestContext,
) (*ext_proc.ProcessingResponse, error) {
	// Accumulate body chunks
	ctx.OriginalRequestBody = append(ctx.OriginalRequestBody, v.RequestBody.Body...)

	// If not end of stream, return CONTINUE to accumulate more chunks
	if !v.RequestBody.EndOfStream {
		return &ext_proc.ProcessingResponse{
			Response: &ext_proc.ProcessingResponse_RequestBody{
				RequestBody: &ext_proc.BodyResponse{
					Response: &ext_proc.CommonResponse{
						Status: ext_proc.CommonResponse_CONTINUE,
					},
				},
			},
		}, nil
	}

	// End of stream - process the complete body
	logging.Infof("[REQ_BODY] Processing complete request body (%d bytes)", len(ctx.OriginalRequestBody))

	// Extract user content from the OpenAI request body
	userContent := extractUserContent(ctx.OriginalRequestBody)
	if userContent == "" {
		logging.Infof("[REQ_BODY] No user content found, using default model")
		return r.createContinueResponse(ctx.OriginalRequestBody), nil
	}

	logging.Infof("[REQ_BODY] User content: %s", truncateString(userContent, 100))

	// Perform domain classification
	detectedDomain, confidence, err := r.Classifier.ClassifyDomain(userContent)
	if err != nil {
		logging.Warnf("[REQ_BODY] Domain classification failed: %v, using default model", err)
		return r.createContinueResponse(ctx.OriginalRequestBody), nil
	}

	logging.Infof("[REQ_BODY] Detected domain: %s (confidence: %.2f)", detectedDomain, confidence)
	ctx.SelectedDomain = detectedDomain

	// Convert config models to decision.ModelMetadata
	models := make(map[string]decision.ModelMetadata)
	for name, m := range r.Config.Models {
		models[name] = decision.ModelMetadata{
			Cost:             m.Cost,
			Domains:          m.Domains,
			AverageLatencyMs: m.AverageLatencyMs,
		}
	}

	// Score models using weighted formula
	weights := decision.ScoringWeights{
		Domain:  r.Config.Weights.Domain,
		Latency: r.Config.Weights.Latency,
		Cost:    r.Config.Weights.Cost,
	}

	scores := decision.ScoreModels(detectedDomain, models, weights)

	// Select the best model
	selectedModel := r.Config.DefaultModel
	if len(scores) > 0 && scores[0].FinalScore > 0 {
		selectedModel = scores[0].ModelName
	}

	logging.Infof("[REQ_BODY] Selected model: %s (domain: %s)", selectedModel, detectedDomain)
	ctx.VSRSelectedModel = selectedModel

	// Rewrite the model in the request body
	modifiedBody := rewriteModelInBody(ctx.OriginalRequestBody, selectedModel)

	// Build score map for logging
	scoreMap := make(map[string]float64)
	for _, s := range scores {
		scoreMap[s.ModelName] = s.FinalScore
	}
	scoreJSON, _ := json.Marshal(map[string]interface{}{
		"selected_model": selectedModel,
		"scores":         scoreMap,
		"domain":         detectedDomain,
	})
	logging.Infof("[REQ_BODY] Routing decision: %s", string(scoreJSON))

	// Create response with body and header mutations
	return r.createRoutingResponse(modifiedBody, selectedModel, detectedDomain), nil
}

// createContinueResponse creates a response that forwards the body unchanged
func (r *OpenAIRouter) createContinueResponse(body []byte) *ext_proc.ProcessingResponse {
	return &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_RequestBody{
			RequestBody: &ext_proc.BodyResponse{
				Response: &ext_proc.CommonResponse{
					Status: ext_proc.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

// createRoutingResponse creates a response with model routing headers and modified body
func (r *OpenAIRouter) createRoutingResponse(body []byte, selectedModel, domain string) *ext_proc.ProcessingResponse {
	return &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_RequestBody{
			RequestBody: &ext_proc.BodyResponse{
				Response: &ext_proc.CommonResponse{
					HeaderMutation: &ext_proc.HeaderMutation{
						SetHeaders: []*core.HeaderValueOption{
							{
								Header: &core.HeaderValue{
									Key:      headers.VSRSelectedModel,
									RawValue: []byte(selectedModel),
								},
							},
							{
								Header: &core.HeaderValue{
									Key:      "x-vsr-selected-domain",
									RawValue: []byte(domain),
								},
							},
							{
								Header: &core.HeaderValue{
									Key:      "content-length",
									RawValue: []byte(fmt.Sprintf("%d", len(body))),
								},
							},
						},
					},
					BodyMutation: &ext_proc.BodyMutation{
						Mutation: &ext_proc.BodyMutation_Body{
							Body: body,
						},
					},
				},
			},
		},
	}
}

// extractUserContent extracts user message content from an OpenAI chat request body
func extractUserContent(body []byte) string {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}

	messages, ok := request["messages"].([]interface{})
	if !ok {
		return ""
	}

	var userContents []string
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role != "user" {
			continue
		}
		content, ok := msgMap["content"].(string)
		if ok {
			userContents = append(userContents, content)
		}
	}

	return strings.Join(userContents, " ")
}

// rewriteModelInBody replaces the "model" field in the JSON request body
func rewriteModelInBody(body []byte, newModel string) []byte {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return body
	}

	request["model"] = newModel

	modified, err := json.Marshal(request)
	if err != nil {
		return body
	}

	return modified
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
