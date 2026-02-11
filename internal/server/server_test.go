package server_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/server"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func testParams() server.Params {
	return server.Params{
		Name:           "testservice",
		PortFromConfig: func(_ *config.Config) int { return 0 },
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln := newTestListener(t)
	addr := ln.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, testParams(), server.Listeners{HTTP: ln})
	}()

	waitForHealthy(t, addr)

	// Trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(domain.GracefulShutdownTimeout + 5*time.Second):
		t.Fatal("shutdown did not complete within budget")
	}
}

func TestRunShutdownCompletesWithinBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln := newTestListener(t)
	addr := ln.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, testParams(), server.Listeners{HTTP: ln})
	}()

	waitForHealthy(t, addr)

	start := time.Now()
	cancel()

	select {
	case <-errCh:
		elapsed := time.Since(start)
		if elapsed > domain.GracefulShutdownTimeout {
			t.Errorf("shutdown took %v, exceeds %v budget", elapsed, domain.GracefulShutdownTimeout)
		}
	case <-time.After(domain.GracefulShutdownTimeout + 5*time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestHealthCheckReturns503DuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln := newTestListener(t)
	addr := ln.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, testParams(), server.Listeners{HTTP: ln})
	}()

	waitForHealthy(t, addr)

	// Trigger shutdown
	cancel()

	// Health check should return 503 during drain delay (before server stops).
	eventually(t, 2*time.Second, func() bool {
		resp, err := httpGet(t, fmt.Sprintf("http://%s/healthz", addr))
		if err != nil {
			return false // server may have already stopped
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusServiceUnavailable
	})

	<-errCh // wait for clean exit
}

func TestRunSetupCallbackInvoked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln := newTestListener(t)
	addr := ln.Addr().String()

	var setupCalled bool
	var receivedDeps server.SetupDeps

	params := server.Params{
		Name:           "testservice",
		PortFromConfig: func(_ *config.Config) int { return 0 },
		Setup: func(_ context.Context, deps server.SetupDeps) (func(context.Context) error, error) {
			setupCalled = true
			receivedDeps = deps
			return nil, nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, params, server.Listeners{HTTP: ln})
	}()

	waitForHealthy(t, addr)

	if !setupCalled {
		t.Fatal("Setup callback was not invoked")
	}
	if receivedDeps.Config == nil {
		t.Error("SetupDeps.Config is nil")
	}
	if receivedDeps.Logger == nil {
		t.Error("SetupDeps.Logger is nil")
	}
	if receivedDeps.HTTPMux == nil {
		t.Error("SetupDeps.HTTPMux is nil")
	}
	// No GRPCPortFromConfig → GRPCServer should be nil.
	if receivedDeps.GRPCServer != nil {
		t.Error("SetupDeps.GRPCServer should be nil when GRPCPortFromConfig is nil")
	}

	cancel()
	<-errCh
}

func TestRunSetupErrorPreventsStart(t *testing.T) {
	ln := newTestListener(t)

	setupErr := errors.New("setup failed")
	params := server.Params{
		Name:           "testservice",
		PortFromConfig: func(_ *config.Config) int { return 0 },
		Setup: func(_ context.Context, _ server.SetupDeps) (func(context.Context) error, error) {
			return nil, setupErr
		},
	}

	err := server.Run(context.Background(), params, server.Listeners{HTTP: ln})
	if err == nil {
		t.Fatal("expected error from setup, got nil")
	}
	if !errors.Is(err, setupErr) {
		t.Errorf("expected setup error to be wrapped, got: %v", err)
	}
}

func TestRunWithGRPCServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	httpLn := newTestListener(t)
	grpcLn := newTestListener(t)
	httpAddr := httpLn.Addr().String()
	grpcAddr := grpcLn.Addr().String()

	var grpcServerFromSetup *grpc.Server

	params := server.Params{
		Name:               "testservice",
		PortFromConfig:     func(_ *config.Config) int { return 0 },
		GRPCPortFromConfig: func(_ *config.Config) int { return 0 },
		Setup: func(_ context.Context, deps server.SetupDeps) (func(context.Context) error, error) {
			grpcServerFromSetup = deps.GRPCServer
			// Register the health service so we can probe gRPC.
			healthpb.RegisterHealthServer(deps.GRPCServer, &stubHealthServer{})
			return nil, nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, params, server.Listeners{HTTP: httpLn, GRPC: grpcLn})
	}()

	// Wait for HTTP to be ready.
	waitForHealthy(t, httpAddr)

	// Verify gRPC server was passed to Setup.
	if grpcServerFromSetup == nil {
		t.Fatal("GRPCServer was nil in SetupDeps")
	}

	// Verify gRPC server is accepting connections.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()

	client := healthpb.NewHealthClient(conn)
	eventually(t, 5*time.Second, func() bool {
		resp, callErr := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
		return callErr == nil && resp.GetStatus() == healthpb.HealthCheckResponse_SERVING
	})

	// Shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(domain.GracefulShutdownTimeout + 5*time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestRunCleanupCalledDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln := newTestListener(t)
	addr := ln.Addr().String()

	cleanupCalled := make(chan struct{})

	params := server.Params{
		Name:           "testservice",
		PortFromConfig: func(_ *config.Config) int { return 0 },
		Setup: func(_ context.Context, _ server.SetupDeps) (func(context.Context) error, error) {
			return func(_ context.Context) error {
				close(cleanupCalled)
				return nil
			}, nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx, params, server.Listeners{HTTP: ln})
	}()

	waitForHealthy(t, addr)
	cancel()

	select {
	case <-cleanupCalled:
		// Cleanup was called during shutdown.
	case <-time.After(domain.GracefulShutdownTimeout + 5*time.Second):
		t.Fatal("cleanup function was not called during shutdown")
	}

	<-errCh
}

// stubHealthServer implements the gRPC Health service for testing.
type stubHealthServer struct {
	healthpb.UnimplementedHealthServer
}

func (s *stubHealthServer) Check(_ context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

// newTestListener creates a TCP listener on an OS-assigned port.
func newTestListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create test listener: %v", err)
	}
	return ln
}

// waitForHealthy polls the health endpoint until it returns 200.
func waitForHealthy(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpGet(t, fmt.Sprintf("http://%s/healthz", addr))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s not healthy within 5s", addr)
}

// httpGet performs an HTTP GET with a background context (satisfies noctx linter).
func httpGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// eventually retries f until it returns true or timeout expires.
func eventually(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
