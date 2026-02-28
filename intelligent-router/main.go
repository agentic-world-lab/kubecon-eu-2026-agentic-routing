package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	service_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// ── Output header names ───────────────────────────────────────────────────────

const (
	headerSelectedModel  = "x-vsr-selected-model"
	headerSelectedDomain = "x-vsr-selected-domain"
	headerModelScores    = "x-vsr-model-scores"
)

// ── CLI flags ─────────────────────────────────────────────────────────────────

var (
	grpcPort    = flag.String("grpcport", ":18080", "gRPC listen address")
	metricsPort = flag.String("metricsport", ":9091", "Prometheus metrics listen address")

	// Unified CR config (new format: IntelligentRouterConfig).
	crPath = flag.String("cr", "", "path to IntelligentRouterConfig CR YAML (new unified format)")

	// Separate IntelligentPool + IntelligentRoute CRs.
	poolPath  = flag.String("pool", "", "path to IntelligentPool CR YAML")
	routePath = flag.String("route", "", "path to IntelligentRoute CR YAML")

	// Legacy combined config (backward compatible).
	configPath = flag.String("config", "config.yaml", "path to legacy combined router config YAML")
)

// ── Prometheus metrics ────────────────────────────────────────────────────────

var (
	routingDecisionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "intelligent_router_decisions_total",
		Help: "Total routing decisions by selected model and detected domain.",
	}, []string{"selected_model", "domain"})

	modelScoreGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "intelligent_router_score",
		Help: "Last computed routing score per model.",
	}, []string{"model"})

	tokensUsedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "intelligent_router_tokens_used",
		Help: "Current tokens used in window by api_key.",
	}, []string{"api_key"})

	budgetPressureGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "intelligent_router_budget_pressure",
		Help: "Current budget pressure [0,1] by api_key.",
	}, []string{"api_key"})

	modelLatencyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "intelligent_router_latency_ms",
		Help: "Current tracked average latency in ms per model.",
	}, []string{"model"})
)

func init() {
	prometheus.MustRegister(
		routingDecisionsTotal,
		modelScoreGauge,
		tokensUsedGauge,
		budgetPressureGauge,
		modelLatencyGauge,
	)
}

// ── Router state (hot-swappable) ──────────────────────────────────────────────

// routerState groups the config and classifiers so they can be swapped atomically
// on every config-file change without restarting the server.
type routerState struct {
	config       *RouterConfig
	classifier   *KeywordClassifier
	mlClassifier *MLClassifier // nil when CGO disabled or ml_classifier.enabled=false
}

// loadState builds a routerState from the given file paths.
// Priority: pool+route > cr > config (legacy).
func loadState(cfgFile, crFile, poolFile, routeFile string) (*routerState, error) {
	var cfg *RouterConfig
	var err error

	if poolFile != "" && routeFile != "" {
		cfg, err = LoadFromPoolAndRoute(poolFile, routeFile)
		if err != nil {
			return nil, fmt.Errorf("pool/route load: %w", err)
		}
		log.Printf("[config] loaded from IntelligentPool=%s + IntelligentRoute=%s", poolFile, routeFile)
	} else if crFile != "" {
		cfg, err = LoadFromCR(crFile)
		if err != nil {
			return nil, fmt.Errorf("CR load: %w", err)
		}
		log.Printf("[config] loaded from IntelligentRouterConfig=%s", crFile)
	} else {
		cfg, err = LoadConfig(cfgFile)
		if err != nil {
			return nil, fmt.Errorf("config load: %w", err)
		}
		log.Printf("[config] loaded from %s", cfgFile)
	}

	kc, err := NewKeywordClassifier(cfg.KeywordRules)
	if err != nil {
		return nil, fmt.Errorf("classifier init: %w", err)
	}

	// Initialise ML classifier if configured.
	var mlc *MLClassifier
	if cfg.MLClassifier.Enabled {
		mlc, err = NewMLClassifier(
			cfg.MLClassifier.ModelPath,
			cfg.MLClassifier.MappingPath,
			cfg.MLClassifier.NumClasses,
			cfg.MLClassifier.UseCPU,
			cfg.MLClassifier.Threshold,
		)
		if err != nil {
			log.Printf("[config] ML classifier disabled: %v", err)
			mlc = nil
		}
	}

	return &routerState{config: cfg, classifier: kc, mlClassifier: mlc}, nil
}

// ── ExtProc server ────────────────────────────────────────────────────────────

// server implements the Envoy ExtProc ExternalProcessorServer interface.
type server struct {
	current        atomic.Pointer[routerState]
	tokenStore     *TokenStore
	latencyTracker *LatencyTracker
}

func newServer(cfgFile, crFile, poolFile, routeFile string) (*server, error) {
	state, err := loadState(cfgFile, crFile, poolFile, routeFile)
	if err != nil {
		return nil, err
	}

	// Seed the latency tracker with initial values from the config.
	initial := make(map[string]float64, len(state.config.Models))
	for name, m := range state.config.Models {
		initial[name] = m.InitialAverageLatencyMs
	}

	window := time.Duration(state.config.TokenBudget.WindowSeconds) * time.Second
	if window <= 0 {
		window = 60 * time.Second
	}

	s := &server{
		tokenStore:     NewTokenStore(window),
		latencyTracker: NewLatencyTracker(initial),
	}
	s.current.Store(state)
	return s, nil
}

// streamState holds all per-stream data accumulated across gRPC messages.
type streamState struct {
	apiKey           string
	bodyBuffer       []byte
	requestStartTime time.Time
	// Classification result set in RequestBody phase, used in ResponseHeaders phase.
	selectedModel string
	domain        string
	scoresJSON    []byte
}

// Process is the main gRPC streaming handler called by Envoy for every request.
func (s *server) Process(srv service_ext_proc_v3.ExternalProcessor_ProcessServer) error {
	log.Printf("[ext_proc] new stream")
	ctx := srv.Context()

	st := &streamState{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv: %v", err)
		}

		var resp *service_ext_proc_v3.ProcessingResponse

		switch v := req.Request.(type) {

		// ── Request headers ───────────────────────────────────────────────
		case *service_ext_proc_v3.ProcessingRequest_RequestHeaders:
			st.apiKey = extractAPIKey(v.RequestHeaders)
			st.requestStartTime = time.Now()
			log.Printf("[ext_proc] RequestHeaders  api_key=%s", maskKey(st.apiKey))
			// Pass through immediately; body arrives next in streaming chunks.
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_RequestHeaders{
					RequestHeaders: &service_ext_proc_v3.HeadersResponse{
						Response: &service_ext_proc_v3.CommonResponse{},
					},
				},
			}

		// ── Request body ──────────────────────────────────────────────────
		case *service_ext_proc_v3.ProcessingRequest_RequestBody:
			h := v.RequestBody
			st.bodyBuffer = append(st.bodyBuffer, h.Body...)

			var reqSetHeaders []*core_v3.HeaderValueOption

			if h.EndOfStream {
				// Full body received. Classify and store result in stream state.
				state := s.current.Load()
				var pressure float64
				if state.config.TokenBudget.Enabled {
					pressure = s.tokenStore.GetPressure(st.apiKey, state.config.TokenBudget)
				}
				log.Printf("[ext_proc] api_key=%s budget_pressure=%.3f", maskKey(st.apiKey), pressure)
				budgetPressureGauge.WithLabelValues(maskKey(st.apiKey)).Set(pressure)

				latencyMs := s.latencyTracker.Snapshot()
				var scores []ModelScore
				st.selectedModel, st.domain, scores = route(st.bodyBuffer, state, latencyMs, pressure)
				log.Printf("[ext_proc] domain=%s selected_model=%s", st.domain, st.selectedModel)

				routingDecisionsTotal.WithLabelValues(st.selectedModel, st.domain).Inc()
				for _, sc := range scores {
					modelScoreGauge.WithLabelValues(sc.ModelName).Set(sc.FinalScore)
				}

				estimatedInput := estimateRequestTokens(st.bodyBuffer)
				s.tokenStore.AddTokens(st.apiKey, estimatedInput)
				newTotal := s.tokenStore.GetTotal(st.apiKey)
				tokensUsedGauge.WithLabelValues(maskKey(st.apiKey)).Set(float64(newTotal))
				log.Printf("[ext_proc] api_key=%s estimated_input=%d new_total=%d",
					maskKey(st.apiKey), estimatedInput, newTotal)

				st.scoresJSON, _ = json.Marshal(scores)

				// Inject x-vsr routing headers into the REQUEST so AgentGateway
				// can use them to decide the upstream route.
				if st.selectedModel != "" {
					reqSetHeaders = []*core_v3.HeaderValueOption{
						{Header: &core_v3.HeaderValue{Key: headerSelectedModel, RawValue: []byte(st.selectedModel)}},
						{Header: &core_v3.HeaderValue{Key: headerSelectedDomain, RawValue: []byte(st.domain)}},
						{Header: &core_v3.HeaderValue{Key: headerModelScores, RawValue: st.scoresJSON}},
					}
				}
			}

			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_RequestBody{
					RequestBody: &service_ext_proc_v3.BodyResponse{
						Response: &service_ext_proc_v3.CommonResponse{
							Status:         service_ext_proc_v3.CommonResponse_CONTINUE,
							HeaderMutation: &service_ext_proc_v3.HeaderMutation{SetHeaders: reqSetHeaders},
							// Echo body bytes via StreamedResponse so agentgateway (streaming mode)
							// can forward them downstream. Mutation_Body triggers the warning
							// "Body() not valid for streaming mode" and silently drops the body.
							BodyMutation: &service_ext_proc_v3.BodyMutation{
								Mutation: &service_ext_proc_v3.BodyMutation_StreamedResponse{
									StreamedResponse: &service_ext_proc_v3.StreamedBodyResponse{
										Body:        h.Body,
										EndOfStream: h.EndOfStream,
									},
								},
							},
						},
					},
				},
			}

		// ── Response headers ──────────────────────────────────────────────
		case *service_ext_proc_v3.ProcessingRequest_ResponseHeaders:
			log.Printf("[ext_proc] ResponseHeaders  domain=%s model=%s", st.domain, st.selectedModel)

			// Update latency tracker with the observed round-trip time.
			if st.selectedModel != "" && !st.requestStartTime.IsZero() {
				elapsedMs := float64(time.Since(st.requestStartTime).Milliseconds())
				s.latencyTracker.Record(st.selectedModel, elapsedMs)
				modelLatencyGauge.WithLabelValues(st.selectedModel).Set(elapsedMs)
			}

			// Inject x-vsr routing headers into the response so the client can
			// observe the classification result.
			var setHeaders []*core_v3.HeaderValueOption
			if st.selectedModel != "" {
				setHeaders = []*core_v3.HeaderValueOption{
					{Header: &core_v3.HeaderValue{Key: headerSelectedModel, RawValue: []byte(st.selectedModel)}},
					{Header: &core_v3.HeaderValue{Key: headerSelectedDomain, RawValue: []byte(st.domain)}},
					{Header: &core_v3.HeaderValue{Key: headerModelScores, RawValue: st.scoresJSON}},
				}
			}
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &service_ext_proc_v3.HeadersResponse{
						Response: &service_ext_proc_v3.CommonResponse{
							HeaderMutation: &service_ext_proc_v3.HeaderMutation{
								SetHeaders: setHeaders,
							},
						},
					},
				},
			}

		// ── Response body ─────────────────────────────────────────────────
		case *service_ext_proc_v3.ProcessingRequest_ResponseBody:
			rb := v.ResponseBody
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_ResponseBody{
					ResponseBody: &service_ext_proc_v3.BodyResponse{
						Response: &service_ext_proc_v3.CommonResponse{
							Status: service_ext_proc_v3.CommonResponse_CONTINUE,
							// Echo response body bytes via StreamedResponse (agentgateway streaming mode).
							BodyMutation: &service_ext_proc_v3.BodyMutation{
								Mutation: &service_ext_proc_v3.BodyMutation_StreamedResponse{
									StreamedResponse: &service_ext_proc_v3.StreamedBodyResponse{
										Body:        rb.Body,
										EndOfStream: rb.EndOfStream,
									},
								},
							},
						},
					},
				},
			}

		case *service_ext_proc_v3.ProcessingRequest_RequestTrailers:
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_RequestTrailers{},
			}

		case *service_ext_proc_v3.ProcessingRequest_ResponseTrailers:
			resp = &service_ext_proc_v3.ProcessingResponse{
				Response: &service_ext_proc_v3.ProcessingResponse_ResponseTrailers{},
			}

		default:
			log.Printf("[ext_proc] unknown request type: %T", v)
			continue
		}

		if err := srv.Send(resp); err != nil {
			log.Printf("[ext_proc] send error: %v", err)
			return err
		}
	}
}

// ── Routing logic ─────────────────────────────────────────────────────────────

// route classifies the request body, scores all models, and returns the
// routing decision.  latencyMs is a snapshot of current per-model tracked
// latency; budgetPressure in [0,1] is supplied by the token store.
func route(body []byte, state *routerState, latencyMs map[string]float64, budgetPressure float64) (
	selectedModel, domain string, scores []ModelScore,
) {
	cfg := state.config
	selectedModel = cfg.DefaultModel

	userContent := extractUserContent(body)
	if userContent == "" {
		domain = "unknown"
		return
	}

	// 1. Keyword-based domain classification.
	if state.classifier != nil {
		if d, _ := state.classifier.Classify(userContent); d != "" {
			domain = d
		}
	}

	// 2. ML classifier fallback (only when keyword produces no match).
	if domain == "" && state.mlClassifier != nil {
		if d, conf := state.mlClassifier.Classify(userContent); d != "" {
			log.Printf("[route] ML classifier: domain=%s confidence=%.3f", d, conf)
			domain = d
		}
	}

	if domain == "" {
		domain = "unknown"
	}

	scores = ScoreModels(domain, cfg.Models, latencyMs, cfg.Weights, budgetPressure, cfg.TokenBudget.Enabled)

	if len(scores) > 0 {
		selectedModel = scores[0].ModelName
	}
	return
}

// extractUserContent pulls the concatenated "user" role messages from an
// OpenAI-compatible chat completion request body.
func extractUserContent(body []byte) string {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}
	messages, ok := request["messages"].([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "user" {
			continue
		}
		if content, ok := msg["content"].(string); ok {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, " ")
}

// ── Token-tracking helpers ────────────────────────────────────────────────────

// maskKey returns the last 8 characters of a key with "..." prefix, for safe logging.
func maskKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return "..." + key[len(key)-8:]
}

// extractAPIKey reads the Bearer token from the Authorization header.
func extractAPIKey(headers *service_ext_proc_v3.HttpHeaders) string {
	if headers == nil || headers.Headers == nil {
		return "unknown"
	}
	for _, h := range headers.Headers.Headers {
		if strings.ToLower(h.Key) == "authorization" {
			val := h.Value
			if len(h.RawValue) > 0 {
				val = string(h.RawValue)
			}
			val = strings.TrimSpace(val)
			if strings.HasPrefix(strings.ToLower(val), "bearer ") {
				return strings.TrimSpace(val[7:])
			}
			return val
		}
	}
	return "unknown"
}

// estimateRequestTokens estimates the number of tokens in the request body.
func estimateRequestTokens(body []byte) int64 {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return estimate(string(body))
	}
	msgs, ok := req["messages"].([]interface{})
	if !ok {
		return estimate(string(body))
	}
	var total int64
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := msg["content"].(string); ok {
			total += estimate(content)
		}
	}
	if total == 0 {
		total = 1
	}
	return total
}

func estimate(text string) int64 {
	n := int64(len(text)) / 4
	if n == 0 && len(text) > 0 {
		n = 1
	}
	return n
}

// ── Config hot-reload ─────────────────────────────────────────────────────────

// watchConfig polls all config files every 5 seconds and atomically swaps
// routerState on any change.
func watchConfig(ctx context.Context, cfgFile, crFile, poolFile, routeFile string, srv *server) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	modTimes := make(map[string]time.Time)
	for _, f := range []string{cfgFile, crFile, poolFile, routeFile} {
		if f == "" {
			continue
		}
		if info, err := os.Stat(f); err == nil {
			modTimes[f] = info.ModTime()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := false
			for f := range modTimes {
				info, err := os.Stat(f)
				if err != nil {
					continue
				}
				if info.ModTime().After(modTimes[f]) {
					modTimes[f] = info.ModTime()
					changed = true
				}
			}
			if !changed {
				continue
			}
			// Log which file(s) triggered the reload.
			for f := range modTimes {
				log.Printf("[config] change detected: %s", f)
			}
			state, err := loadState(cfgFile, crFile, poolFile, routeFile)
			if err != nil {
				log.Printf("[config] reload failed: %v", err)
				continue
			}
			// Ensure latency tracker has entries for any new models.
			for name, m := range state.config.Models {
				srv.latencyTracker.EnsureModel(name, m.InitialAverageLatencyMs)
			}
			srv.current.Store(state)
			cfg := state.config
			modelNames := make([]string, 0, len(cfg.Models))
			for n := range cfg.Models {
				modelNames = append(modelNames, n)
			}
			log.Printf("[config] hot-reloaded: default_model=%s models=%v keyword_rules=%d",
				cfg.DefaultModel, modelNames, len(cfg.KeywordRules))
		}
	}
}

// ── Health server ─────────────────────────────────────────────────────────────

type healthServer struct{}

func (h *healthServer) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (h *healthServer) Watch(_ *grpc_health_v1.HealthCheckRequest, srv grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch not implemented")
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	switch {
	case *poolPath != "" && *routePath != "":
		log.Printf("Starting intelligent-router  mode=CR  pool=%s  route=%s  grpc=%s  metrics=%s",
			*poolPath, *routePath, *grpcPort, *metricsPort)
	case *crPath != "":
		log.Printf("Starting intelligent-router  mode=CR  cr=%s  grpc=%s  metrics=%s",
			*crPath, *grpcPort, *metricsPort)
	default:
		log.Printf("Starting intelligent-router  mode=legacy  config=%s  grpc=%s  metrics=%s",
			*configPath, *grpcPort, *metricsPort)
	}

	srv, err := newServer(*configPath, *crPath, *poolPath, *routePath)
	if err != nil {
		log.Fatalf("failed to initialise server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchConfig(ctx, *configPath, *crPath, *poolPath, *routePath, srv)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("Prometheus metrics on %s/metrics", *metricsPort)
		if err := http.ListenAndServe(*metricsPort, mux); err != nil {
			log.Printf("metrics server error: %v", err)
		}
	}()

	lis, err := net.Listen("tcp", *grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *grpcPort, err)
	}
	grpcSrv := grpc.NewServer(grpc.MaxConcurrentStreams(1000))
	service_ext_proc_v3.RegisterExternalProcessorServer(grpcSrv, srv)
	grpc_health_v1.RegisterHealthServer(grpcSrv, &healthServer{})
	log.Printf("gRPC server on %s", *grpcPort)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-stop
		log.Printf("received signal %v, shutting down", sig)
		grpcSrv.GracefulStop()
		cancel()
	}()

	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
