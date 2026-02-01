// Package main is the entrypoint for the Fanout service.
// Fanout consumes from Kafka and delivers messages to connected clients via Gateway.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/observability"
)

const serviceName = "fanout"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Initialize structured logging with secret redaction
	logger := observability.InitLogger(observability.LogConfig{
		Level:       cfg.LogLevel,
		Format:      cfg.LogFormat,
		ServiceName: serviceName,
		Environment: cfg.Environment,
	})

	// Initialize OpenTelemetry tracer
	tracerProvider, err := observability.InitTracer(ctx, observability.TracerConfig{
		ServiceName:    serviceName,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return fmt.Errorf("initialize tracer: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tracerProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("failed to shutdown tracer", slog.String("error", shutdownErr.Error()))
		}
	}()

	// Initialize OpenTelemetry metrics
	metricsProvider, err := observability.InitMetrics(ctx, observability.MetricsConfig{
		ServiceName:    serviceName,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return fmt.Errorf("initialize metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := metricsProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("failed to shutdown metrics", slog.String("error", shutdownErr.Error()))
		}
	}()

	// Setup HTTP server with health check
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Fanout.HTTPPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server",
			slog.Int("port", cfg.Fanout.HTTPPort),
			slog.String("environment", cfg.Environment),
		)
		if listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
		}
	}()

	// Wait for shutdown signal or error
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case serverErr := <-errCh:
		return fmt.Errorf("server error: %w", serverErr)
	case sig := <-sigCh:
		logger.Info("received shutdown signal", slog.String("signal", sig.String()))
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), domain.GracefulShutdownTimeout)
	defer cancel()

	logger.Info("starting graceful shutdown")
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}

// healthHandler responds to health check requests.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy","service":"fanout"}`))
}
