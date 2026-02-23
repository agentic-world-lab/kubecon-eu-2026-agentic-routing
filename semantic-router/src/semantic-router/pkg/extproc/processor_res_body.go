package extproc

import (
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// handleResponseBody processes the response body
func (r *OpenAIRouter) handleResponseBody(
	v *ext_proc.ProcessingRequest_ResponseBody,
	ctx *RequestContext,
) (*ext_proc.ProcessingResponse, error) {
	endOfStream := v.ResponseBody.GetEndOfStream()

	if endOfStream {
		logging.Infof("[RES_BODY] End of response stream")
	}

	// Forward the response body unchanged using streaming
	return &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_ResponseBody{
			ResponseBody: &ext_proc.BodyResponse{
				Response: &ext_proc.CommonResponse{
					BodyMutation: &ext_proc.BodyMutation{
						Mutation: &ext_proc.BodyMutation_StreamedResponse{
							StreamedResponse: &ext_proc.StreamedBodyResponse{
								Body:        v.ResponseBody.Body,
								EndOfStream: endOfStream,
							},
						},
					},
				},
			},
		},
	}, nil
}
