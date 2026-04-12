package observe

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/osama1998H/moca/internal/config"
)

// stubTracingConfig returns a TracingConfig suitable for unit tests.
// The OTLP exporter uses lazy connection, so no running collector is required.
func stubTracingConfig() config.TracingConfig {
	return config.TracingConfig{
		Enabled:    true,
		Exporter:   "otlp",
		Endpoint:   "localhost:4317",
		SampleRate: 1.0,
	}
}

func TestTracer_ReturnsNonNil(t *testing.T) {
	// Install a no-op provider for isolated test.
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := Tracer("test.component")
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
}

func TestShutdownTracer_NoError(t *testing.T) {
	tp := sdktrace.NewTracerProvider()

	if err := ShutdownTracer(context.Background(), tp); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}

func TestInitTracer_ReturnsProvider(t *testing.T) {
	// We can't easily test a real OTLP connection in unit tests, so we
	// verify that InitTracer returns a non-nil provider without dialing.
	// The OTLP exporter uses lazy connection by default, so construction
	// succeeds even without a running collector.
	ctx := context.Background()
	cfg := stubTracingConfig()

	tp, err := InitTracer(ctx, cfg, "moca-test", "0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer func() { _ = ShutdownTracer(ctx, tp) }()
}
