package tracing_test

import (
	"context"
	"slices"
	"testing"
	"time"

	moduletracing "github.com/nanostack-dev/nanostack-framework/modules/tracing"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

func restoreGlobals(t *testing.T) {
	t.Helper()
	provider, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
	})
}

func TestModuleWithoutEndpointIsNoOp(t *testing.T) {
	restoreGlobals(t)
	t.Setenv(tracing.EnvExporterEndpoint, "")
	t.Setenv(tracing.EnvTracesEndpoint, "")
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	var provider trace.TracerProvider
	app := fx.New(
		fx.NopLogger,
		fx.Supply(zerolog.Nop()),
		moduletracing.Module,
		fx.Populate(&provider),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, span := provider.Tracer("test").Start(context.Background(), "op")
	if span.IsRecording() {
		t.Fatal("expected a no-op provider without an endpoint")
	}
	if fields := otel.GetTextMapPropagator().Fields(); !slices.Contains(fields, "traceparent") {
		t.Fatalf("W3C propagator not installed, fields = %v", fields)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestModuleFlushesOnStop(t *testing.T) {
	restoreGlobals(t)
	exporter := tracetest.NewInMemoryExporter()
	var provider trace.TracerProvider
	var spans int
	app := fx.New(
		fx.NopLogger,
		fx.Supply(zerolog.Nop()),
		moduletracing.Module,
		fx.Decorate(func(tracing.Config) tracing.Config { return tracing.Config{Enabled: true, SampleRatio: 1} }),
		fx.Provide(fx.Annotate(
			func() tracing.Option { return tracing.WithExporter(countingExporter{exporter, &spans}) },
			fx.ResultTags(`group:"tracing.options"`),
		)),
		fx.Populate(&provider),
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, span := provider.Tracer("test").Start(ctx, "op")
	span.End()
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if spans != 1 {
		t.Fatalf("exported %d spans on stop, want 1", spans)
	}
}

type countingExporter struct {
	*tracetest.InMemoryExporter
	count *int
}

func (e countingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	*e.count += len(spans)
	return e.InMemoryExporter.ExportSpans(ctx, spans)
}
