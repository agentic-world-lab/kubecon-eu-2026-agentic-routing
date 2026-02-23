package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	candle_binding "github.com/vllm-project/semantic-router/candle-binding"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/config"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/extproc"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

func main() {
	var (
		configPath  = flag.String("config", "config/config.yaml", "Path to the configuration file")
		port        = flag.Int("port", 50051, "Port to listen on for gRPC ExtProc")
		metricsPort = flag.Int("metrics-port", 9190, "Port for Prometheus metrics")
	)
	flag.Parse()

	// Initialize logging
	if _, err := logging.InitLoggerFromEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
	}

	// Check if config file exists
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		logging.Fatalf("Config file not found: %s", *configPath)
	}

	// Load configuration
	cfg, err := config.Parse(*configPath)
	if err != nil {
		logging.Fatalf("Failed to load config: %v", err)
	}
	config.Replace(cfg)

	// Initialize embedding models (Qwen3 via candle-binding) for domain classification
	if cfg.EmbeddingModels.Qwen3ModelPath != "" || cfg.EmbeddingModels.GemmaModelPath != "" {
		logging.Infof("Initializing embedding models: qwen3=%q, gemma=%q, useCPU=%t",
			cfg.EmbeddingModels.Qwen3ModelPath, cfg.EmbeddingModels.GemmaModelPath, cfg.EmbeddingModels.UseCPU)

		initErr := candle_binding.InitEmbeddingModels(
			cfg.EmbeddingModels.Qwen3ModelPath,
			cfg.EmbeddingModels.GemmaModelPath,
			"", // no mmbert
			cfg.EmbeddingModels.UseCPU,
		)
		if initErr != nil {
			logging.Warnf("Failed to initialize embedding models: %v", initErr)
			logging.Warnf("Domain classification via embeddings will not be available")
		} else {
			logging.Infof("Embedding models initialized successfully")
		}
	} else {
		logging.Infof("No embedding models configured, skipping initialization")
	}

	// Start metrics server if enabled
	metricsEnabled := true
	if cfg.Observability.Metrics.Enabled != nil {
		metricsEnabled = *cfg.Observability.Metrics.Enabled
	}
	if metricsEnabled && *metricsPort > 0 {
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			metricsAddr := fmt.Sprintf(":%d", *metricsPort)
			logging.Infof("Starting metrics server on %s", metricsAddr)
			if metricsErr := http.ListenAndServe(metricsAddr, nil); metricsErr != nil {
				logging.Errorf("Metrics server error: %v", metricsErr)
			}
		}()
	}

	// Create and start the ExtProc server
	server, err := extproc.NewServer(*configPath, *port)
	if err != nil {
		logging.Fatalf("Failed to create ExtProc server: %v", err)
	}

	logging.Infof("Starting Model Router with config: %s", *configPath)
	logging.Infof("Models configured: %d, Default model: %s", len(cfg.Models), cfg.DefaultModel)

	if err := server.Start(); err != nil {
		logging.Fatalf("ExtProc server error: %v", err)
	}
}
