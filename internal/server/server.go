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
	"google.golang.org/grpc"

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

	// GRPCPortFromConfig extracts the gRPC port for this service from config.
	// When nil, no gRPC server is started.
	GRPCPortFromConfig func(cfg *config.Config) int

	// Setup is called after config, logging, and observability are initialized
	// but before the servers start accepting connections. Use it to register
	// gRPC services, mount grpc-gateway handlers, or perform other service-
	// specific initialization.
	//
	// The returned cleanup function (if non-nil) is called during graceful
	// shutdown after HTTP and gRPC servers stop but before OTEL flush. Use
	// it to close infrastructure clients, wait on background goroutines, etc.
	//
	// When Setup is nil, no setup or cleanup is performed.
	Setup func(ctx context.Context, deps SetupDeps) (cleanup func(context.Context) error, err error)
}

// SetupDeps holds the dependencies available to a service's Setup callback.
type SetupDeps struct {
	Config     *config.Config
	Logger     *slog.Logger
	HTTPMux    *http.ServeMux
	GRPCServer *grpc.Server // nil if GRPCPortFromConfig is nil
}

// Listeners holds optional pre-created listeners for testing (port-0).
// Zero-value fields cause Run to create listeners from config.
type Listeners struct {
	HTTP net.Listener
	GRPC net.Listener
}

// Run executes the full service lifecycle: signal handling, config loading,
// observability initialization, HTTP server with health checks, optional gRPC
// server, and graceful shutdown. Listeners fields override config-based
// listener creation (enables port-0 testing).
func Run(ctx context.Context, p Params, lns Listeners) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observability.InitLogger(observability.LogConfig{
		Level:       cfg.LogLevel,
		Format:      cfg.LogFormat,
		ServiceName: p.Name,
		Environment: cfg.Environment,
	})

	tp, mp, err := initOTEL(ctx, p.Name, cfg)
	if err != nil {
		return err
	}

	var shuttingDown atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler(&shuttingDown, p.Name))

	grpcServer := newGRPCServerIfConfigured(p)

	var cleanupFn func(context.Context) error
	if p.Setup != nil {
		var setupErr error
		cleanupFn, setupErr = p.Setup(ctx, SetupDeps{
			Config:     cfg,
			Logger:     logger,
			HTTPMux:    mux,
			GRPCServer: grpcServer,
		})
		if setupErr != nil {
			return fmt.Errorf("setup: %w", setupErr)
		}
	}

	httpLn, err := resolveListener(ctx, lns.HTTP, p.PortFromConfig, cfg, "http")
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	grpcLn, err := resolveGRPCListener(ctx, lns.GRPC, p, cfg, grpcServer)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)
	startServers(g, logger, httpSrv, httpLn, grpcServer, grpcLn, cfg.Environment)
	g.Go(shutdownFunc(ctx, logger, &shuttingDown, httpSrv, grpcServer, cleanupFn, tp, mp))

	return g.Wait()
}

// initOTEL initializes tracer and metrics providers.
func initOTEL(ctx context.Context, name string, cfg *config.Config) (
	*observability.TracerProvider, *observability.MetricsProvider, error,
) {
	tp, err := observability.InitTracer(ctx, observability.TracerConfig{
		ServiceName:    name,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize tracer: %w", err)
	}

	mp, err := observability.InitMetrics(ctx, observability.MetricsConfig{
		ServiceName:    name,
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTEL.Endpoint,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize metrics: %w", err)
	}

	return tp, mp, nil
}

// healthHandler returns an HTTP handler for the /healthz endpoint.
func healthHandler(shuttingDown *atomic.Bool, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"shutting_down","service":%q}`, name)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","service":%q}`, name)
	}
}

// newGRPCServerIfConfigured creates a gRPC server when GRPCPortFromConfig is set.
func newGRPCServerIfConfigured(p Params) *grpc.Server {
	if p.GRPCPortFromConfig == nil {
		return nil
	}
	return grpc.NewServer()
}

// resolveListener returns the injected listener or creates one from config.
func resolveListener(
	ctx context.Context, injected net.Listener, portFn func(*config.Config) int,
	cfg *config.Config, protocol string,
) (net.Listener, error) {
	if injected != nil {
		return injected, nil
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", fmt.Sprintf(":%d", portFn(cfg)))
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", protocol, err)
	}
	return ln, nil
}

// resolveGRPCListener returns the gRPC listener when a gRPC server is configured.
func resolveGRPCListener(
	ctx context.Context, injected net.Listener, p Params,
	cfg *config.Config, grpcServer *grpc.Server,
) (net.Listener, error) {
	if grpcServer == nil {
		return nil, nil
	}
	return resolveListener(ctx, injected, p.GRPCPortFromConfig, cfg, "grpc")
}

// startServers adds HTTP and optional gRPC goroutines to the errgroup.
func startServers(
	g *errgroup.Group, logger *slog.Logger,
	httpSrv *http.Server, httpLn net.Listener,
	grpcServer *grpc.Server, grpcLn net.Listener,
	environment string,
) {
	g.Go(func() error {
		logger.Info("starting HTTP server",
			slog.String("addr", httpLn.Addr().String()),
			slog.String("environment", environment),
		)
		if serveErr := httpSrv.Serve(httpLn); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	})

	if grpcServer != nil {
		g.Go(func() error {
			logger.Info("starting gRPC server",
				slog.String("addr", grpcLn.Addr().String()),
			)
			if serveErr := grpcServer.Serve(grpcLn); serveErr != nil {
				return fmt.Errorf("grpc serve: %w", serveErr)
			}
			return nil
		})
	}
}

// shutdownFunc returns the errgroup function that orchestrates graceful shutdown.
// Shutdown order: gRPC GracefulStop -> HTTP Shutdown -> service cleanup -> OTEL flush.
func shutdownFunc(
	ctx context.Context, logger *slog.Logger, shuttingDown *atomic.Bool,
	httpSrv *http.Server, grpcServer *grpc.Server,
	cleanupFn func(context.Context) error,
	tp *observability.TracerProvider, mp *observability.MetricsProvider,
) func() error {
	return func() error {
		<-ctx.Done()
		logger.Info("received shutdown signal, starting graceful shutdown")

		shuttingDown.Store(true)
		time.Sleep(domain.ShutdownDrainDelay)

		if grpcServer != nil {
			grpcServer.GracefulStop()
			logger.Info("gRPC server stopped")
		}

		httpCtx, httpCancel := context.WithTimeout(context.Background(), domain.ShutdownHTTPTimeout)
		defer httpCancel()
		if shutdownErr := httpSrv.Shutdown(httpCtx); shutdownErr != nil {
			logger.Error("HTTP server shutdown error", slog.String("error", shutdownErr.Error()))
		}

		if cleanupFn != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), domain.ShutdownHTTPTimeout)
			defer cleanupCancel()
			if cleanupErr := cleanupFn(cleanupCtx); cleanupErr != nil {
				logger.Error("service cleanup error", slog.String("error", cleanupErr.Error()))
			}
		}

		otelCtx, otelCancel := context.WithTimeout(context.Background(), domain.ShutdownOTELTimeout)
		defer otelCancel()
		if shutdownErr := mp.Shutdown(otelCtx); shutdownErr != nil {
			logger.Error("failed to shutdown metrics", slog.String("error", shutdownErr.Error()))
		}
		if shutdownErr := tp.Shutdown(otelCtx); shutdownErr != nil {
			logger.Error("failed to shutdown tracer", slog.String("error", shutdownErr.Error()))
		}

		logger.Info("shutdown complete")
		return nil
	}
}
