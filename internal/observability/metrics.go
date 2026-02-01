package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// MetricsConfig holds configuration for the metrics provider.
type MetricsConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string // Empty string disables OTLP export
}

// MetricsProvider wraps the OpenTelemetry meter provider with shutdown capabilities.
type MetricsProvider struct {
	provider *sdkmetric.MeterProvider
}

// InitMetrics initializes the OpenTelemetry meter provider.
// Returns a MetricsProvider that must be shut down on application exit.
func InitMetrics(ctx context.Context, cfg MetricsConfig) (*MetricsProvider, error) {
	// Create resource with service attributes only (avoid schema conflicts with resource.Default())
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	)

	var opts []sdkmetric.Option
	opts = append(opts, sdkmetric.WithResource(res))

	// Configure exporter if endpoint is provided
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(), // TODO: Configure TLS for production
		)
		if err != nil {
			return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	}

	provider := sdkmetric.NewMeterProvider(opts...)

	// Set global meter provider
	otel.SetMeterProvider(provider)

	return &MetricsProvider{provider: provider}, nil
}

// Shutdown flushes any remaining metrics and shuts down the provider.
func (mp *MetricsProvider) Shutdown(ctx context.Context) error {
	if mp.provider == nil {
		return nil
	}
	return mp.provider.Shutdown(ctx)
}

// Meter returns a meter for the given instrumentation name.
func Meter(name string) metric.Meter {
	return otel.Meter(name)
}
