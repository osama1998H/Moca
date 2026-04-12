package observe

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

	"github.com/osama1998H/moca/internal/config"
)

// InitTracer creates an OTLP gRPC trace exporter and installs a global
// TracerProvider with ratio-based sampling. The returned provider must be
// stored by the caller and shut down via ShutdownTracer during graceful
// shutdown to flush pending spans.
func InitTracer(ctx context.Context, cfg config.TracingConfig, serviceName, version string) (*sdktrace.TracerProvider, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 1.0
	}

	// Build exporter options.
	exporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.IsInsecure() {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create tracing resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

// Tracer returns an OpenTelemetry Tracer for the given instrumentation name.
// This is a convenience wrapper so callers don't need to import the otel
// package directly.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// ShutdownTracer flushes any pending spans and shuts down the TracerProvider.
// Should be called during graceful server shutdown.
func ShutdownTracer(ctx context.Context, tp *sdktrace.TracerProvider) error {
	return tp.Shutdown(ctx)
}
