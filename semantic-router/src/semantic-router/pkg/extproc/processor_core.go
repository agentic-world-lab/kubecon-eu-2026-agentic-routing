package extproc

import (
	"context"
	"errors"
	"io"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/headers"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// Process implements the ext_proc ExternalProcessor_ProcessServer interface
func (r *OpenAIRouter) Process(stream ext_proc.ExternalProcessor_ProcessServer) error {
	logging.Infof("Processing at stage [init]")

	ctx := &RequestContext{
		Headers: make(map[string]string),
	}

	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				logging.Infof("Stream ended gracefully")
				return nil
			}
			if s, ok := status.FromError(err); ok {
				switch s.Code() {
				case codes.Canceled:
					return nil
				case codes.DeadlineExceeded:
					logging.Infof("Stream deadline exceeded")
					return nil
				}
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}

			logging.Errorf("Error receiving request: %v", err)
			return err
		}

		switch v := req.Request.(type) {
		case *ext_proc.ProcessingRequest_RequestHeaders:
			response, err := r.handleRequestHeaders(v, ctx)
			if err != nil {
				logging.Errorf("handleRequestHeaders failed: %v", err)
				return err
			}
			// Defer response until request body is processed (Wait for Body Pattern)
			ctx.DeferredHeaderResponse = response
			logging.Infof("[PROCESSOR_CORE] Deferring headers response until body is processed")

		case *ext_proc.ProcessingRequest_RequestBody:
			response, err := r.handleRequestBody(v, ctx)
			if err != nil {
				logging.Errorf("handleRequestBody failed: %v", err)
				return err
			}

			// If deferring and not end of stream, continue accumulating
			if ctx.DeferredHeaderResponse != nil && !v.RequestBody.EndOfStream {
				logging.Infof("[PROCESSOR_CORE] Deferring intermediate body chunk")
				continue
			}

			// End of stream: send deferred headers first, then body response
			if v.RequestBody.EndOfStream && ctx.DeferredHeaderResponse != nil && !ctx.HeaderResponseSent {
				logging.Infof("[PROCESSOR_CORE] Sending deferred headers with routing decision")

				// Update the deferred header response with the selected model
				if ctx.VSRSelectedModel != "" {
					if respHeaders, ok := ctx.DeferredHeaderResponse.Response.(*ext_proc.ProcessingResponse_RequestHeaders); ok {
						if respHeaders.RequestHeaders.Response == nil {
							respHeaders.RequestHeaders.Response = &ext_proc.CommonResponse{}
						}
						if respHeaders.RequestHeaders.Response.HeaderMutation == nil {
							respHeaders.RequestHeaders.Response.HeaderMutation = &ext_proc.HeaderMutation{}
						}

						// Replace the selected model header
						var newHeaders []*core.HeaderValueOption
						for _, h := range respHeaders.RequestHeaders.Response.HeaderMutation.SetHeaders {
							if h.Header != nil && h.Header.Key != headers.VSRSelectedModel {
								newHeaders = append(newHeaders, h)
							}
						}
						newHeaders = append(newHeaders, &core.HeaderValueOption{
							Header: &core.HeaderValue{
								Key:      headers.VSRSelectedModel,
								RawValue: []byte(ctx.VSRSelectedModel),
							},
						})
						respHeaders.RequestHeaders.Response.HeaderMutation.SetHeaders = newHeaders
					}
				}

				if err := sendResponse(stream, ctx.DeferredHeaderResponse, "deferred request header"); err != nil {
					return err
				}
				ctx.HeaderResponseSent = true
				ctx.DeferredHeaderResponse = nil
			}

			if err := sendResponse(stream, response, "request body"); err != nil {
				return err
			}

		case *ext_proc.ProcessingRequest_ResponseHeaders:
			response, err := r.handleResponseHeaders(v, ctx)
			if err != nil {
				return err
			}
			if err := sendResponse(stream, response, "response header"); err != nil {
				return err
			}

		case *ext_proc.ProcessingRequest_ResponseBody:
			response, err := r.handleResponseBody(v, ctx)
			if err != nil {
				return err
			}
			if err := sendResponse(stream, response, "response body"); err != nil {
				return err
			}

		default:
			logging.Warnf("Unknown request type: %v", v)
			response := &ext_proc.ProcessingResponse{
				Response: &ext_proc.ProcessingResponse_RequestBody{
					RequestBody: &ext_proc.BodyResponse{
						Response: &ext_proc.CommonResponse{
							Status: ext_proc.CommonResponse_CONTINUE,
						},
					},
				},
			}
			if err := sendResponse(stream, response, "unknown"); err != nil {
				return err
			}
		}
	}
}
