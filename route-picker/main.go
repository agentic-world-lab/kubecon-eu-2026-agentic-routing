// based on https://github.com/salrashid123/envoy_ext_proc/blob/eca3b3a89929bf8cb80879ba553798ecea1c5622/grpc_server.go

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	service_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/open-policy-agent/opa/rego"
	"github.com/solo-io/go-utils/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	grpcport = flag.String("grpcport", ":18080", "grpcport")
	// Header name to add for routing decision. Set via env var ROUTING_HEADER_NAME.
	routingHeaderName = "x-route-picker"
	// Policy file path and default route value (configurable via env)
	routingPolicyPath = "policies/policy.rego"
	defaultRoute      = "cpu"

	// Prepared policy and mutex for safe concurrent access
	policyMu      sync.RWMutex
	preparedQuery *rego.PreparedEvalQuery
)

type Instructions struct {
	// Header key/value pairs to add to the request or response.
	AddHeaders map[string]string `json:"addHeaders"`
	// Header keys to remove from the request or response.
	RemoveHeaders []string `json:"removeHeaders"`
	// Set the body of the request or response to the specified string. If empty, will be ignored.
	SetBody string `json:"setBody"`
	// Set the request or response trailers.
	SetTrailers map[string]string `json:"setTrailers"`
}

type server struct{}

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, in *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	log.Printf("Handling grpc Check request: + %s", in.String())
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(in *grpc_health_v1.HealthCheckRequest, srv grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func (s *healthServer) List(ctx context.Context, in *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{
		Statuses: map[string]*grpc_health_v1.HealthCheckResponse{
			"": {Status: grpc_health_v1.HealthCheckResponse_SERVING},
		},
	}, nil
}

func (s *server) Process(srv service_ext_proc_v3.ExternalProcessor_ProcessServer) error {
	log.Printf("Process")
	ctx := srv.Context()
	for {
		select {
		case <-ctx.Done():
			log.Printf("context done")
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			// envoy has closed the stream. Don't return anything and close this stream entirely
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		// Log oneof type for quick reference
		log.Printf("ProcessingRequest oneof type: %T", req.Request)

		// If we received RequestHeaders, print a single concise, human-friendly
		// log with header keys and decoded values (do not print both encoded
		// protojson and decoded values).
		if rh, ok := req.Request.(*service_ext_proc_v3.ProcessingRequest_RequestHeaders); ok {
			log.Printf("Received RequestHeaders:")
			for _, h := range rh.RequestHeaders.Headers.Headers {
				// Prefer RawValue (decoded bytes) if present, otherwise use Value string
				if len(h.RawValue) > 0 {
					log.Printf("  header: %s: %s", h.Key, string(h.RawValue))
				} else if h.Value != "" {
					log.Printf("  header: %s: %s", h.Key, h.Value)
				} else {
					log.Printf("  header: %s: <empty>", h.Key)
				}
			}
			// Show the instructions JSON (if any) once
			if instr := getInstructionsFromHeaders(rh.RequestHeaders); instr != "" {
				log.Printf("  instructions: %s", instr)
			}
		} else {
			// For other request types, keep a compact log. If the nested value
			// is a proto.Message, marshal to JSON once; otherwise print its Go type.
			if req.Request == nil {
				log.Printf("ProcessingRequest.Request is nil")
			} else if m, ok := req.Request.(proto.Message); ok {
				if jb, err := protojson.Marshal(m); err != nil {
					log.Printf("nested request marshal error: %v", err)
				} else {
					log.Printf("Nested request JSON: %s", string(jb))
				}
			} else {
				log.Printf("nested request is not proto.Message: %T", req.Request)
			}
		}

		// build response based on request type
		resp := &service_ext_proc_v3.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *service_ext_proc_v3.ProcessingRequest_RequestHeaders:
			log.Printf("Got RequestHeaders")

			h := req.Request.(*service_ext_proc_v3.ProcessingRequest_RequestHeaders)
			headersResp, err := getHeadersResponseFromInstructions(h.RequestHeaders)
			if err != nil {
				return err
			}
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_RequestHeaders{
					RequestHeaders: headersResp,
				},
			}

		case *service_ext_proc_v3.ProcessingRequest_RequestBody:
			log.Printf("Got RequestBody - forwarding")

			h := req.Request.(*service_ext_proc_v3.ProcessingRequest_RequestBody)

			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_RequestBody{
					RequestBody: &service_ext_proc_v3.BodyResponse{
						Response: &service_ext_proc_v3.CommonResponse{
							BodyMutation: &service_ext_proc_v3.BodyMutation{
								Mutation: &service_ext_proc_v3.BodyMutation_StreamedResponse{
									StreamedResponse: &service_ext_proc_v3.StreamedBodyResponse{
										Body:        h.RequestBody.Body,
										EndOfStream: h.RequestBody.EndOfStream,
									},
								},
							},
						},
					},
				},
			}
		case *service_ext_proc_v3.ProcessingRequest_RequestTrailers:
			log.Printf("Got RequestTrailers (not currently handled)")
			resp.Response = &service_ext_proc_v3.ProcessingResponse_RequestTrailers{}

		case *service_ext_proc_v3.ProcessingRequest_ResponseHeaders:
			log.Printf("Got ResponseHeaders")

			h := req.Request.(*service_ext_proc_v3.ProcessingRequest_ResponseHeaders)
			headersResp, err := getHeadersResponseFromInstructions(h.ResponseHeaders)
			if err != nil {
				return err
			}
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: headersResp,
				},
			}

		case *service_ext_proc_v3.ProcessingRequest_ResponseBody:
			log.Printf("Got ResponseBody - forwarding")

			h := req.Request.(*service_ext_proc_v3.ProcessingRequest_ResponseBody)

			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_ResponseBody{
					ResponseBody: &service_ext_proc_v3.BodyResponse{
						Response: &service_ext_proc_v3.CommonResponse{
							BodyMutation: &service_ext_proc_v3.BodyMutation{
								Mutation: &service_ext_proc_v3.BodyMutation_StreamedResponse{
									StreamedResponse: &service_ext_proc_v3.StreamedBodyResponse{
										Body:        h.ResponseBody.Body,
										EndOfStream: h.ResponseBody.EndOfStream,
									},
								},
							},
						},
					},
				},
			}

		case *service_ext_proc_v3.ProcessingRequest_ResponseTrailers:
			log.Printf("Got ResponseTrailers (not currently handled)")
			resp.Response = &service_ext_proc_v3.ProcessingResponse_ResponseTrailers{}

		default:
			log.Printf("Unknown Request type %v", v)
		}

		// At this point we believe we have created a valid response...
		// note that this is sometimes not the case
		// anyways for now just send it
		log.Printf("Sending ProcessingResponse")

		// Try to extract the routing header value (decoded) so we can log it
		var pickedRoute string
		if resp != nil && resp.Response != nil {
			switch rr := resp.Response.(type) {
			case *service_ext_proc_v3.ProcessingResponse_RequestHeaders:
				if rr.RequestHeaders != nil && rr.RequestHeaders.Response != nil && rr.RequestHeaders.Response.HeaderMutation != nil {
					for _, h := range rr.RequestHeaders.Response.HeaderMutation.SetHeaders {
						if h != nil && h.Header != nil && h.Header.Key == routingHeaderName {
							if len(h.Header.RawValue) > 0 {
								pickedRoute = string(h.Header.RawValue)
							} else {
								pickedRoute = h.Header.Value
							}
							break
						}
					}
				}
			case *service_ext_proc_v3.ProcessingResponse_ResponseHeaders:
				if rr.ResponseHeaders != nil && rr.ResponseHeaders.Response != nil && rr.ResponseHeaders.Response.HeaderMutation != nil {
					for _, h := range rr.ResponseHeaders.Response.HeaderMutation.SetHeaders {
						if h != nil && h.Header != nil && h.Header.Key == routingHeaderName {
							if len(h.Header.RawValue) > 0 {
								pickedRoute = string(h.Header.RawValue)
							} else {
								pickedRoute = h.Header.Value
							}
							break
						}
					}
				}
			}
		}
		if pickedRoute != "" {
			log.Printf("The route picked is: %s", pickedRoute)
		}
		// Marshal response to JSON for easier inspection in logs (shows header mutations)
		if jb, err := protojson.Marshal(resp); err != nil {
			log.Printf("error marshaling ProcessingResponse: %v", err)
		} else {
			log.Printf("ProcessingResponse JSON: %s", string(jb))
		}
		if err := srv.Send(resp); err != nil {
			log.Printf("send error %v", err)
			return err
		}

	}
}

func main() {

	flag.Parse()

	// Require ROUTING_POLICY_FILE and DEFAULT_ROUTE environment variables.
	// Crash if not set.
	if v := os.Getenv("ROUTING_POLICY_FILE"); v != "" {
		routingPolicyPath = v
	} else {
		log.Fatalf("ROUTING_POLICY_FILE environment variable is required")
	}
	if v := os.Getenv("DEFAULT_ROUTE"); v != "" {
		defaultRoute = v
	} else {
		log.Fatalf("DEFAULT_ROUTE environment variable is required")
	}

	// Allow overriding the routing header name via environment variable.
	if v := os.Getenv("ROUTING_HEADER_NAME"); v != "" {
		routingHeaderName = v
	}

	// Load and compile policy at startup. If missing or compile fails, do
	// not crash the server — log the error and continue using the
	// `DEFAULT_ROUTE` fallback. The policy may be hot-reloaded later.
	if err := loadPolicy(routingPolicyPath); err != nil {
		log.Printf("warning: failed to load policy %s: %v; continuing with DEFAULT_ROUTE=%s", routingPolicyPath, err, defaultRoute)
	}

	// Start file watcher for hot-reload of policy (errors will be logged).
	go watchPolicyFile(routingPolicyPath)

	lis, err := net.Listen("tcp", *grpcport)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	sopts := []grpc.ServerOption{grpc.MaxConcurrentStreams(1000)}
	s := grpc.NewServer(sopts...)

	service_ext_proc_v3.RegisterExternalProcessorServer(s, &server{})

	grpc_health_v1.RegisterHealthServer(s, &healthServer{})

	log.Printf("Starting gRPC server on port %s", *grpcport)

	var gracefulStop = make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		log.Printf("caught sig: %+v", sig)
		time.Sleep(time.Second)
		log.Printf("Graceful stop completed")
		os.Exit(0)
	}()
	err = s.Serve(lis)
	if err != nil {
		log.Fatalf("killing server with %v", err)
	}
}

func getInstructionsFromHeaders(in *service_ext_proc_v3.HttpHeaders) string {
	for _, n := range in.Headers.Headers {
		if n.Key == "instructions" {
			return string(n.RawValue)
		}
	}
	return ""
}

func getHeadersResponseFromInstructions(in *service_ext_proc_v3.HttpHeaders) (*service_ext_proc_v3.HeadersResponse, error) {
	instructionString := getInstructionsFromHeaders(in)

	// If no instructions were provided, treat it as an empty Instructions
	// object so we can still apply default behavior (e.g., add routing header).
	var instructions *Instructions
	if instructionString == "" {
		instructions = &Instructions{}
	} else {
		var err error
		instructions = &Instructions{}
		err = json.Unmarshal([]byte(instructionString), &instructions)
		if err != nil {
			log.Printf("Error unmarshalling instructions: %v", err)
			return nil, err
		}
	}

	// build the response
	resp := &service_ext_proc_v3.HeadersResponse{
		Response: &service_ext_proc_v3.CommonResponse{},
	}

	// headers
	if len(instructions.AddHeaders) > 0 || len(instructions.RemoveHeaders) > 0 {
		var addHeaders []*core_v3.HeaderValueOption
		for k, v := range instructions.AddHeaders {
			addHeaders = append(addHeaders, &core_v3.HeaderValueOption{
				Header: &core_v3.HeaderValue{Key: k, RawValue: []byte(v)},
			})
		}
		resp.Response.HeaderMutation = &service_ext_proc_v3.HeaderMutation{
			SetHeaders:    addHeaders,
			RemoveHeaders: instructions.RemoveHeaders,
		}
	}

	// Always ensure `x-routing-decision: cpu` is added unless the instructions
	// explicitly set that header. This makes the server add the header for every
	// call by default.
	var hasRoutingDecision bool
	if instructions != nil && len(instructions.AddHeaders) > 0 {
		if _, ok := instructions.AddHeaders[routingHeaderName]; ok {
			hasRoutingDecision = true
		}
	}
	if !hasRoutingDecision {
		// Evaluate policy to determine header value. If evaluation fails,
		// fall back to configured defaultRoute.
		routeValue := evaluatePolicy(in)

		if resp.Response.HeaderMutation == nil {
			resp.Response.HeaderMutation = &service_ext_proc_v3.HeaderMutation{}
		}
		resp.Response.HeaderMutation.SetHeaders = append(resp.Response.HeaderMutation.SetHeaders,
			&core_v3.HeaderValueOption{Header: &core_v3.HeaderValue{Key: routingHeaderName, RawValue: []byte(routeValue)}},
		)
	}

	// body
	if instructions.SetBody != "" {
		body := []byte(instructions.SetBody)

		if resp.Response.HeaderMutation == nil {
			resp.Response.HeaderMutation = &service_ext_proc_v3.HeaderMutation{}
		}
		resp.Response.HeaderMutation.SetHeaders = append(resp.Response.HeaderMutation.SetHeaders,
			[]*core_v3.HeaderValueOption{
				{
					Header: &core_v3.HeaderValue{
						Key:   "content-type",
						Value: "text/plain",
					},
				},
				{
					Header: &core_v3.HeaderValue{
						Key:   "Content-Length",
						Value: strconv.Itoa(len(body)),
					},
				},
			}...)
		resp.Response.BodyMutation = &service_ext_proc_v3.BodyMutation{
			Mutation: &service_ext_proc_v3.BodyMutation_Body{
				Body: body,
			},
		}
	}

	// trailers
	if len(instructions.SetTrailers) > 0 {
		var setTrailers []*core_v3.HeaderValue
		for k, v := range instructions.SetTrailers {
			setTrailers = append(setTrailers, &core_v3.HeaderValue{Key: k, Value: v})
		}
		resp.Response.Trailers = &core_v3.HeaderMap{
			Headers: setTrailers,
		}
	}

	return resp, nil
}

// loadPolicy reads and compiles the Rego policy at the given path and stores a
// prepared query in the global preparedQuery protected by policyMu.
func loadPolicy(path string) error {
	log.Printf("Loading policy from %s", path)
	bs, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := rego.New(
		rego.Query("data.routepicker.header_value"),
		rego.Module(path, string(bs)),
	)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return err
	}

	// Sanity-evaluate the prepared query with an empty headers map to catch
	// obvious runtime errors in the policy (e.g., type errors). If this
	// evaluation returns an error, do not install the new policy.
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer testCancel()
	_, err = pq.Eval(testCtx, rego.EvalInput(map[string]interface{}{"headers": map[string]string{}}))
	if err != nil {
		return fmt.Errorf("policy compile succeeded but eval failed: %w", err)
	}

	policyMu.Lock()
	preparedQuery = &pq
	policyMu.Unlock()
	log.Printf("Policy loaded and prepared")
	return nil
}

// watchPolicyFile watches the directory containing the policy file and reloads
// the policy when the file is modified, renamed, or created.
func watchPolicyFile(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("failed to create fs watcher: %v", err)
		return
	}
	defer watcher.Close()

	dir := filepath.Dir(path)

	// Watch the containing directory to detect rewrites/renames.
	if err := watcher.Add(dir); err != nil {
		log.Printf("failed to add watcher on %s: %v", dir, err)
		return
	}

	// Also watch ..data symlink for ConfigMap updates in Kubernetes
	dataDir := filepath.Join(dir, "..data")
	_ = watcher.Add(dataDir) // ignore error if ..data doesn't exist

	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Reload on any relevant event in the directory.
			// For ConfigMap updates, Kubernetes rotates the ..data symlink,
			// so we trigger reload on Create/Write events in the directory.
			shouldReload := false
			if ev.Name == path && (ev.Op&fsnotify.Write == fsnotify.Write || ev.Op&fsnotify.Create == fsnotify.Create) {
				shouldReload = true
			}
			// Also watch for ..data symlink changes (ConfigMap atomic updates)
			if filepath.Base(ev.Name) == "..data" && (ev.Op&fsnotify.Create == fsnotify.Create || ev.Op&fsnotify.Remove == fsnotify.Remove) {
				shouldReload = true
			}

			if shouldReload {
				log.Printf("policy file change detected: %v", ev)
				// Small delay to allow Kubernetes to finish the atomic symlink swap
				time.Sleep(100 * time.Millisecond)
				if err := loadPolicy(path); err != nil {
					// Clear any previously loaded policy so the server uses the
					// DEFAULT_ROUTE fallback for subsequent requests.
					policyMu.Lock()
					preparedQuery = nil
					policyMu.Unlock()
					log.Printf("failed to reload policy: %v; cleared policy and using DEFAULT_ROUTE=%s", err, defaultRoute)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

// evaluatePolicy runs the prepared query with a simple input constructed from
// the request headers. Returns the route string or defaultRoute on error.
func evaluatePolicy(in *service_ext_proc_v3.HttpHeaders) string {
	// Build input headers map
	input := map[string]interface{}{"headers": map[string]string{}}
	hdrs := input["headers"].(map[string]string)
	for _, h := range in.Headers.Headers {
		if len(h.RawValue) > 0 {
			hdrs[h.Key] = string(h.RawValue)
		} else {
			hdrs[h.Key] = h.Value
		}
	}

	policyMu.RLock()
	pq := preparedQuery
	policyMu.RUnlock()
	if pq == nil {
		log.Printf("no prepared policy query available; using default route %s", defaultRoute)
		return defaultRoute
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rs, err := pq.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		log.Printf("policy evaluation error: %v; using default %s", err, defaultRoute)
		return defaultRoute
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		log.Printf("policy returned no result; using default %s", defaultRoute)
		return defaultRoute
	}
	val := rs[0].Expressions[0].Value
	if s, ok := val.(string); ok {
		return s
	}
	// If it's not a string, try to marshal to JSON and use that as a fallback.
	jb, err := json.Marshal(val)
	if err != nil {
		log.Printf("policy result non-string and cannot marshal; using default %s", defaultRoute)
		return defaultRoute
	}
	return string(jb)
}
