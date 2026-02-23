package extproc

import (
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/headers"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// RequestContext holds the context for processing a request
type RequestContext struct {
	Headers             map[string]string
	RequestID           string
	RequestPath         string
	Method              string
	OriginalRequestBody []byte
	RequestModel        string

	// Timing
	StartTime           time.Time
	ProcessingStartTime time.Time

	// Streaming state
	IsStreamingRequest  bool
	IsStreamingResponse bool
	StreamingComplete   bool
	StreamingAborted    bool

	// Deferred header response (Wait for Body pattern)
	DeferredHeaderResponse *ext_proc.ProcessingResponse
	HeaderResponseSent     bool

	// Routing decision
	VSRSelectedModel string
	SelectedDomain   string
}

// handleRequestHeaders processes incoming request headers
func (r *OpenAIRouter) handleRequestHeaders(
	v *ext_proc.ProcessingRequest_RequestHeaders,
	ctx *RequestContext,
) (*ext_proc.ProcessingResponse, error) {
	ctx.StartTime = time.Now()
	ctx.ProcessingStartTime = time.Now()

	// Extract headers
	if v.RequestHeaders != nil && v.RequestHeaders.Headers != nil {
		for _, h := range v.RequestHeaders.Headers.Headers {
			val := h.Value
			if len(h.RawValue) > 0 {
				val = string(h.RawValue)
			}
			ctx.Headers[h.Key] = val

			switch h.Key {
			case ":path":
				ctx.RequestPath = val
			case ":method":
				ctx.Method = val
			case "x-request-id":
				ctx.RequestID = val
			}
		}
	}

	logging.Infof("[REQ_HEADERS] path=%s method=%s request_id=%s", ctx.RequestPath, ctx.Method, ctx.RequestID)

	// Set default model header for routing
	defaultModel := r.Config.DefaultModel
	if defaultModel == "" {
		defaultModel = "default"
	}

	resp := &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_RequestHeaders{
			RequestHeaders: &ext_proc.HeadersResponse{
				Response: &ext_proc.CommonResponse{
					HeaderMutation: &ext_proc.HeaderMutation{
						SetHeaders: []*core.HeaderValueOption{
							{
								Header: &core.HeaderValue{
									Key:      headers.VSRSelectedModel,
									RawValue: []byte(defaultModel),
								},
							},
						},
					},
				},
			},
		},
	}

	return resp, nil
}
