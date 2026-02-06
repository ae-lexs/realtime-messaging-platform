// Package server provides the shared service lifecycle runner.
// All cmd/ services delegate to server.Run for signal handling,
// config loading, observability init, health checks, and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

// Params configures a service's lifecycle runner.
type Params struct {
	// Name identifies the service (e.g. "gateway", "ingest").
	Name string

	// PortFromConfig extracts the HTTP port for this service from config.
	PortFromConfig func(cfg *config.Config) int
}

// Run executes the full service lifecycle: signal handling, config loading,
// observability initialization, HTTP server with health checks, and graceful
// shutdown. If ln is non-nil, it is used instead of creating a new listener
// from config (enables port-0 testing).
func Run(ctx context.Context, p Params, ln net.Listener) error {
	// Signal-based cancellation: ctx.Done() closes on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Load configuration
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Initialize structured logging with secret redaction
	logger := observability.InitLogger(observability.LogConfig{
		Level:       cfg.LogLevel,
		Format:      cfg.LogFormat,
		ServiceName: p.Name,
		Environment: cfg.Environment,
	})

	// --- Startup order: tracer -> metrics -> HTTP server ---

	// Initialize OpenTelemetry tracer
	tracerProvider, err := observability.InitTracer(ctx, observability.TracerConfig{
		ServiceName:    p.Name,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return fmt.Errorf("initialize tracer: %w", err)
	}

	// Initialize OpenTelemetry metrics
	metricsProvider, err := observability.InitMetrics(ctx, observability.MetricsConfig{
		ServiceName:    p.Name,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return fmt.Errorf("initialize metrics: %w", err)
	}

	// Health check shutdown coordination via atomic flag.
	var shuttingDown atomic.Bool

	// Setup HTTP server with health check
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"shutting_down","service":%q}`, p.Name)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","service":%q}`, p.Name)
	})

	// Bind listener (use injected listener or create from config).
	if ln == nil {
		ln, err = (&net.ListenConfig{}).Listen(ctx, "tcp", fmt.Sprintf(":%d", p.PortFromConfig(cfg)))
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
	}

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Structured concurrency via errgroup ---
	g, ctx := errgroup.WithContext(ctx)

	// Goroutine 1: Serve HTTP
	g.Go(func() error {
		logger.Info("starting HTTP server",
			slog.String("addr", ln.Addr().String()),
			slog.String("environment", cfg.Environment),
		)
		if serveErr := server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	})

	// Goroutine 2: Shutdown trigger — waits for context cancellation, then drains.
	// Shutdown order is explicit reverse of startup: HTTP server -> metrics -> tracer.
	g.Go(func() error {
		<-ctx.Done()
		logger.Info("received shutdown signal, starting graceful shutdown")

		// 1. Mark shutting down — health checks return 503
		shuttingDown.Store(true)

		// 2. Drain delay — let load balancer propagate endpoint removal
		time.Sleep(domain.ShutdownDrainDelay)

		// 3. Drain HTTP server (reverse of startup: HTTP started last, stops first)
		httpCtx, httpCancel := context.WithTimeout(context.Background(), domain.ShutdownHTTPTimeout)
		defer httpCancel()
		if shutdownErr := server.Shutdown(httpCtx); shutdownErr != nil {
			logger.Error("HTTP server shutdown error", slog.String("error", shutdownErr.Error()))
		}

		// 4. Flush OTEL (reverse: metrics first, then tracer)
		otelCtx, otelCancel := context.WithTimeout(context.Background(), domain.ShutdownOTELTimeout)
		defer otelCancel()
		if shutdownErr := metricsProvider.Shutdown(otelCtx); shutdownErr != nil {
			logger.Error("failed to shutdown metrics", slog.String("error", shutdownErr.Error()))
		}
		if shutdownErr := tracerProvider.Shutdown(otelCtx); shutdownErr != nil {
			logger.Error("failed to shutdown tracer", slog.String("error", shutdownErr.Error()))
		}

		logger.Info("shutdown complete")
		return nil
	})

	return g.Wait()
}
