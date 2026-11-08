package nanotrace

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

// recordingExporter keeps every exported span and counts shutdowns.
type recordingExporter struct {
	mu        sync.Mutex
	names     []string
	shutdowns int
}

func (e *recordingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, span := range spans {
		e.names = append(e.names, span.Name())
	}
	return nil
}

func (e *recordingExporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shutdowns++
	return nil
}

func exportingConfig() Config {
	return Config{Endpoint: "http://collector:4318", SampleRatio: 1}
}

func TestStopFlushesBufferedSpans(t *testing.T) {
	exporter := &recordingExporter{}
	provider, err := newProvider(t.Context(), exportingConfig(), log.Settings{Service: "billing"},
		slog.New(slog.DiscardHandler), tracing.WithExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	_, span := TracerProvider(provider).Tracer("test").Start(t.Context(), "work")
	span.End()

	if err = provider.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if len(exporter.names) != 1 || exporter.names[0] != "work" {
		t.Fatalf("exported spans = %v, want [work]: Stop must flush the batch processor", exporter.names)
	}
	if exporter.shutdowns != 1 {
		t.Fatalf("exporter shut down %d times, want 1", exporter.shutdowns)
	}
}

func TestStartInstallsTheGlobalProviderAndPropagator(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	provider, err := newProvider(t.Context(), exportingConfig(), log.Settings{},
		slog.New(slog.DiscardHandler), tracing.WithExporter(&recordingExporter{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	if err = provider.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if otel.GetTracerProvider() != TracerProvider(provider) {
		t.Error("Start did not install the global tracer provider")
	}
	// The composite propagator builds Fields from a map, so their order is
	// random: compare them as a set.
	fields := slices.Sorted(slices.Values(otel.GetTextMapPropagator().Fields()))
	if want := []string{"baggage", "traceparent", "tracestate"}; !slices.Equal(fields, want) {
		t.Errorf("propagator fields = %v, want %v", fields, want)
	}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	}))
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if got, want := carrier.Get("traceparent"), "00-01000000000000000000000000000000-0100000000000000-01"; got != want {
		t.Errorf("injected traceparent = %q, want %q", got, want)
	}
}

func TestExportingFollowsTheOTelEnvironmentRule(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  Config
		want bool
	}{
		"no endpoint":        {Config{}, false},
		"base endpoint":      {Config{Endpoint: "http://c:4318"}, true},
		"traces endpoint":    {Config{TracesEndpoint: "http://c:4318/v1/traces"}, true},
		"sdk disabled":       {Config{Endpoint: "http://c:4318", SDKDisabled: true}, false},
		"exporter none":      {Config{Endpoint: "http://c:4318", TracesExporter: " NONE "}, false},
		"exporter otlp":      {Config{Endpoint: "http://c:4318", TracesExporter: "otlp"}, true},
		"blank endpoint":     {Config{Endpoint: "  "}, false},
		"disabled no export": {Config{SDKDisabled: true}, false},
	} {
		if got := tc.cfg.Exporting(); got != tc.want {
			t.Errorf("%s: Exporting() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestNotExportingGivesANoopProvider(t *testing.T) {
	provider, err := NewProvider(t.Context(), Config{SampleRatio: 1}, log.Settings{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if provider.Exporting() {
		t.Fatal("a provider without an endpoint must not export")
	}
	if err = provider.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestContextAttrsAddsTraceIDs(t *testing.T) {
	provider, err := newProvider(t.Context(), exportingConfig(), log.Settings{},
		slog.New(slog.DiscardHandler), tracing.WithExporter(&recordingExporter{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })
	ctx, span := TracerProvider(provider).Tracer("test").Start(t.Context(), "work")
	defer span.End()

	attrs := ContextAttrs()(ctx)

	if len(attrs) != 2 || attrs[0].Key != log.KeyTraceID || attrs[1].Key != log.KeySpanID {
		t.Fatalf("attrs = %v, want trace_id and span_id", attrs)
	}
	if attrs[0].Value.String() != span.SpanContext().TraceID().String() {
		t.Fatalf("trace_id = %s, want %s", attrs[0].Value, span.SpanContext().TraceID())
	}
}

func TestServiceVersionPrefersTheConfiguredVersion(t *testing.T) {
	if got := serviceVersion(" 1.4.0 "); got != "1.4.0" {
		t.Fatalf("serviceVersion = %q, want 1.4.0", got)
	}
}
