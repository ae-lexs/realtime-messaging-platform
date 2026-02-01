// Package observability provides OpenTelemetry setup for tracing, metrics, and structured logging.
// All services use this package for consistent observability per ADR-012.
package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerConfig holds configuration for the tracer provider.
type TracerConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string // Empty string disables OTLP export (uses noop)
}

// TracerProvider wraps the OpenTelemetry tracer provider with shutdown capabilities.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
}

// InitTracer initializes the OpenTelemetry tracer provider.
// Returns a TracerProvider that must be shut down on application exit.
func InitTracer(ctx context.Context, cfg TracerConfig) (*TracerProvider, error) {
	// Create resource with service attributes only (avoid schema conflicts with resource.Default())
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	)

	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithResource(res))

	// Configure exporter if endpoint is provided
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(), // TODO: Configure TLS for production
		)
		if err != nil {
			return nil, fmt.Errorf("create OTLP exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	provider := sdktrace.NewTracerProvider(opts...)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{provider: provider}, nil
}

// Shutdown flushes any remaining spans and shuts down the provider.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}
	return tp.provider.Shutdown(ctx)
}

// Tracer returns a tracer for the given instrumentation name.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// SpanFromContext extracts the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext extracts the trace ID from context as a string.
// Returns empty string if no trace is active.
func TraceIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.HasTraceID() {
		return ""
	}
	return spanCtx.TraceID().String()
}
