package extproc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/fsnotify/fsnotify"
	"google.golang.org/grpc"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// Server represents a gRPC server for the Envoy ExtProc
type Server struct {
	configPath string
	service    *RouterService
	server     *grpc.Server
	port       int
}

// NewServer creates a new ExtProc gRPC server
func NewServer(configPath string, port int) (*Server, error) {
	router, err := NewOpenAIRouter(configPath)
	if err != nil {
		return nil, err
	}

	service := NewRouterService(router)
	return &Server{
		configPath: configPath,
		service:    service,
		port:       port,
	}, nil
}

// Start starts the gRPC server
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}

	logging.Infof("Starting Model Router ExtProc server on port %d...", s.port)

	s.server = grpc.NewServer()
	ext_proc.RegisterExternalProcessorServer(s.server, s.service)

	// Run the server in a separate goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		if err := s.server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logging.Errorf("Server error: %v", err)
			serverErrCh <- err
		} else {
			serverErrCh <- nil
		}
	}()

	// Start config file watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watchConfigAndReload(ctx)

	// Wait for interrupt signal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		if err != nil {
			return err
		}
	case <-signalChan:
		logging.Infof("Received shutdown signal, stopping server...")
	}

	s.Stop()
	return nil
}

// Stop stops the gRPC server
func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
		logging.Infof("Server stopped")
	}
}

// RouterService delegates gRPC calls to the current router implementation
type RouterService struct {
	current atomic.Pointer[OpenAIRouter]
}

// NewRouterService creates a new RouterService
func NewRouterService(r *OpenAIRouter) *RouterService {
	rs := &RouterService{}
	rs.current.Store(r)
	return rs
}

// Swap replaces the current router implementation
func (rs *RouterService) Swap(r *OpenAIRouter) { rs.current.Store(r) }

// Process delegates to the current router
func (rs *RouterService) Process(stream ext_proc.ExternalProcessor_ProcessServer) error {
	r := rs.current.Load()
	return r.Process(stream)
}

// watchConfigAndReload watches the config file and reloads router on changes
func (s *Server) watchConfigAndReload(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logging.Errorf("Failed to create config watcher: %v", err)
		return
	}
	defer watcher.Close()

	cfgFile := s.configPath
	cfgDir := filepath.Dir(cfgFile)

	if err := watcher.Add(cfgDir); err != nil {
		logging.Errorf("Failed to watch config dir %s: %v", cfgDir, err)
		return
	}
	_ = watcher.Add(cfgFile)

	var (
		pending bool
		last    time.Time
	)

	reload := func() {
		newRouter, err := NewOpenAIRouter(cfgFile)
		if err != nil {
			logging.Errorf("Config reload failed: %v", err)
			return
		}
		s.service.Swap(newRouter)
		logging.Infof("Configuration reloaded from %s", cfgFile)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				if filepath.Base(ev.Name) == filepath.Base(cfgFile) || filepath.Dir(ev.Name) == cfgDir {
					if !pending || time.Since(last) > 250*time.Millisecond {
						pending = true
						last = time.Now()
						go func() { time.Sleep(300 * time.Millisecond); reload() }()
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logging.Errorf("Config watcher error: %v", err)
		}
	}
}
