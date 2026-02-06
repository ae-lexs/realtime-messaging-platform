package observability_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/aelexs/realtime-messaging-platform/internal/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestInitTracer_NoEndpoint(t *testing.T) {
	cfg := observability.TracerConfig{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		OTLPEndpoint:   "",
	}

	tp, err := observability.InitTracer(context.Background(), cfg)

	require.NoError(t, err)
	require.NotNil(t, tp)

	err = tp.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestTracerProvider_ShutdownNilProvider(t *testing.T) {
	tp := &observability.TracerProvider{}

	err := tp.Shutdown(context.Background())

	assert.NoError(t, err)
}

func TestTraceIDFromContext_NoActiveSpan(t *testing.T) {
	traceID := observability.TraceIDFromContext(context.Background())

	assert.Empty(t, traceID)
}

func TestTraceIDFromContext_WithActiveSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	traceID := observability.TraceIDFromContext(ctx)

	assert.NotEmpty(t, traceID)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), traceID)
}

func TestSpanFromContext(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	got := observability.SpanFromContext(ctx)

	assert.NotNil(t, got)
	assert.True(t, got.SpanContext().IsValid())
	assert.Implements(t, (*trace.Span)(nil), got)
}
