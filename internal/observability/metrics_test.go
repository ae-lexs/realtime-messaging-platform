package observability_test

import (
	"context"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMetrics_NoEndpoint(t *testing.T) {
	cfg := observability.MetricsConfig{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		OTLPEndpoint:   "",
	}

	mp, err := observability.InitMetrics(context.Background(), cfg)

	require.NoError(t, err)
	require.NotNil(t, mp)
}

func TestMetricsProvider_ShutdownNilProvider(t *testing.T) {
	mp := &observability.MetricsProvider{}

	err := mp.Shutdown(context.Background())

	assert.NoError(t, err)
}

func TestMetricsProvider_Shutdown(t *testing.T) {
	cfg := observability.MetricsConfig{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		OTLPEndpoint:   "",
	}

	mp, err := observability.InitMetrics(context.Background(), cfg)
	require.NoError(t, err)

	err = mp.Shutdown(context.Background())

	assert.NoError(t, err)
}
