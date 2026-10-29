package tracing_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

func restoreGlobals(t *testing.T) {
	t.Helper()
	provider, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
	})
}

func TestDisabledProviderIsNoOpButPropagates(t *testing.T) {
	restoreGlobals(t)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	provider, err := tracing.New(context.Background(), tracing.Config{Enabled: false, SampleRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Exporting() {
		t.Fatal("disabled provider must not export")
	}
	provider.Install()

	_, span := otel.Tracer("test").Start(context.Background(), "op")
	if span.IsRecording() {
		t.Fatal("disabled provider recorded a span")
	}
	span.End()

	header := http.Header{}
	header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(header))
	if got := trace.SpanContextFromContext(ctx).TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("propagator not installed: extracted trace id %q", got)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEnabledProviderExportsWithResourceFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "echopoint")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=dev")
	exporter := keepingExporter{tracetest.NewInMemoryExporter()}

	provider, err := tracing.New(context.Background(),
		tracing.Config{Enabled: true, SampleRatio: 1, ServiceName: "fallback", ServiceVersion: "v9.9.9"},
		tracing.WithExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	_, span := provider.TracerProvider().Tracer("test").Start(context.Background(), "op")
	span.End()
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
	attrs := map[attribute.Key]string{}
	for _, kv := range spans[0].Resource.Attributes() {
		attrs[kv.Key] = kv.Value.Emit()
	}
	if attrs["service.name"] != "echopoint" {
		t.Fatalf("service.name = %q, want env value to win over fallback", attrs["service.name"])
	}
	if attrs["deployment.environment"] != "dev" {
		t.Fatalf("deployment.environment = %q", attrs["deployment.environment"])
	}
	if attrs["service.version"] != "v9.9.9" {
		t.Fatalf("service.version = %q", attrs["service.version"])
	}
}

func TestSamplerIsParentBasedRatio(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider, err := tracing.New(context.Background(), tracing.Config{Enabled: true, SampleRatio: 0},
		tracing.WithExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	tracer := provider.TracerProvider().Tracer("test")

	_, root := tracer.Start(context.Background(), "root")
	if root.SpanContext().IsSampled() {
		t.Fatal("ratio 0 sampled a new root trace")
	}
	root.End()

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	_, child := tracer.Start(trace.ContextWithRemoteSpanContext(context.Background(), parent), "child")
	if !child.SpanContext().IsSampled() {
		t.Fatal("a sampled remote parent was not honoured")
	}
	child.End()
	_ = provider.Shutdown(context.Background())
}

// An unreachable collector must not slow spans down, and shutdown stays
// bounded by its context.
func TestUnreachableCollectorDoesNotBlock(t *testing.T) {
	clearEnv(t)
	t.Setenv(tracing.EnvExporterEndpoint, "http://127.0.0.1:1")
	provider, err := tracing.New(context.Background(), tracing.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	if !provider.Exporting() {
		t.Fatal("expected an exporting provider")
	}
	tracer := provider.TracerProvider().Tracer("test")

	start := time.Now()
	for range 1000 {
		_, span := tracer.Start(context.Background(), "op")
		span.End()
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("recording spans took %v with no collector", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	_ = provider.Shutdown(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("shutdown took %v, want it bounded by its context", elapsed)
	}
}

// keepingExporter keeps spans across Shutdown, which the in-memory exporter
// otherwise resets.
type keepingExporter struct{ *tracetest.InMemoryExporter }

func (keepingExporter) Shutdown(context.Context) error { return nil }

var _ sdktrace.SpanExporter = keepingExporter{}
