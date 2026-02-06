package server_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aelexs/realtime-messaging-platform/internal/config"
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
	"github.com/aelexs/realtime-messaging-platform/internal/server"
	"go.uber.org/goleak"
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
		errCh <- server.Run(ctx, testParams(), ln)
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
		errCh <- server.Run(ctx, testParams(), ln)
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
		errCh <- server.Run(ctx, testParams(), ln)
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
