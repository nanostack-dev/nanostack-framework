package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Provider owns a TracerProvider and its shutdown.
type Provider struct {
	tracerProvider trace.TracerProvider
	shutdown       func(context.Context) error
	exporting      bool
}

// Option adjusts how New builds the provider.
type Option func(*options)

type options struct {
	exporter sdktrace.SpanExporter
}

// WithExporter replaces the OTLP/HTTP exporter, typically with an in-memory
// exporter in tests. It has no effect when cfg.Enabled is false.
func WithExporter(exporter sdktrace.SpanExporter) Option {
	return func(o *options) { o.exporter = exporter }
}

// New builds the provider described by cfg.
//
// When cfg.Enabled is false it returns a no-op provider: spans are not
// recorded, but an incoming traceparent still flows through the context, so
// logs keep the edge's trace_id and outbound calls keep propagating it.
//
// When enabled, spans go through a batch span processor to the OTLP/HTTP
// exporter. Export runs on a background goroutine with a bounded queue that
// drops on overflow, so an unreachable collector never blocks or fails a
// request. The exporter does not connect at construction.
func New(ctx context.Context, cfg Config, opts ...Option) (*Provider, error) {
	if !cfg.Enabled {
		return &Provider{
			tracerProvider: noop.NewTracerProvider(),
			shutdown:       func(context.Context) error { return nil },
		}, nil
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	exporter := o.exporter
	if exporter == nil {
		otlp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("create otlp trace exporter: %w", err)
		}
		exporter = otlp
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	sdk := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	return &Provider{tracerProvider: sdk, shutdown: sdk.Shutdown, exporting: true}, nil
}

// newResource layers the framework fallbacks under the standard environment
// detector, so OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES win.
func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	var fallbacks []attribute.KeyValue
	if cfg.ServiceName != "" {
		fallbacks = append(fallbacks, attribute.String("service.name", cfg.ServiceName))
	}
	if cfg.ServiceVersion != "" {
		fallbacks = append(fallbacks, attribute.String("service.version", cfg.ServiceVersion))
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(fallbacks...),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}
	return res, nil
}

// TracerProvider returns the provider to pass to instrumentation.
func (p *Provider) TracerProvider() trace.TracerProvider {
	return p.tracerProvider
}

// Exporting reports whether spans leave the process.
func (p *Provider) Exporting() bool {
	return p.exporting
}

// Shutdown flushes buffered spans and stops the exporter. It is bounded by ctx.
func (p *Provider) Shutdown(ctx context.Context) error {
	if err := p.shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown tracer provider: %w", err)
	}
	return nil
}

// Install publishes p as the global TracerProvider and installs the W3C
// propagator. Instrumentation created before Install delegates to it.
func (p *Provider) Install() {
	otel.SetTracerProvider(p.tracerProvider)
	InstallPropagator()
}

// Propagator returns the W3C TraceContext and Baggage propagator.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// InstallPropagator sets Propagator as the global text-map propagator.
func InstallPropagator() {
	otel.SetTextMapPropagator(Propagator())
}
