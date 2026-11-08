package nanotrace

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"runtime/debug"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

// Module provides *Provider, trace.TracerProvider and *Installer, and adds
// ContextAttrs to the []log.ContextAttrs collection.
var Module = nano.Module("tracing", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewProvider, TracerProvider, NewInstaller),
	nano.Collect[log.ContextAttrs](ContextAttrs),
)

// Config decides whether and where spans are exported and how new root
// traces are sampled.
//
// The fields follow the OpenTelemetry environment specification and bind
// from the OTEL_* names or from a YAML layer. TracesEndpoint is the traces
// URL, used as given; Endpoint is the base URL, to which the exporter joins
// /v1/traces. The exporter still reads its headers, timeout and compression
// from the OTEL_* environment variables. Service name and version come from
// log.Settings (SERVICE_NAME and SERVICE_VERSION), the process identity.
//
// A Config built by hand starts from DefaultConfig.
type Config struct {
	// SDKDisabled turns export off (OTEL_SDK_DISABLED=true).
	SDKDisabled bool `config:"sdk_disabled,optional" env:"OTEL_SDK_DISABLED" json:"sdk_disabled" yaml:"sdkDisabled"`
	// TracesExporter "none" turns export off.
	TracesExporter string `config:"traces_exporter,optional" env:"OTEL_TRACES_EXPORTER" json:"traces_exporter" yaml:"tracesExporter"`
	// Endpoint is the OTLP/HTTP base URL, such as http://collector:4318.
	// Spans go to its path joined with /v1/traces. Export needs it or
	// TracesEndpoint.
	Endpoint string `config:"endpoint,optional" env:"OTEL_EXPORTER_OTLP_ENDPOINT" json:"endpoint" yaml:"endpoint"`
	// TracesEndpoint is the full OTLP/HTTP traces URL, used as given. It wins
	// over Endpoint.
	TracesEndpoint string `config:"traces_endpoint,optional" env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" json:"traces_endpoint" yaml:"tracesEndpoint"`
	// SampleRatio is the fraction of new root traces that are sampled. A
	// sampled incoming parent (traceparent) is always kept.
	SampleRatio float64 `config:"sample_ratio" env:"OTEL_TRACES_SAMPLER_ARG" default:"1" validate:"min=0,max=1" json:"sample_ratio" yaml:"sampleRatio"`
}

// DefaultConfig returns the Config of the default: tags: no export, every
// new root trace sampled. Start a Config built by hand, in a test or an
// Override, from it: a zero SampleRatio is valid and samples no root trace.
func DefaultConfig() Config { return Config{SampleRatio: 1} }

// Exporting reports whether c turns export on: an OTLP endpoint is set, the
// SDK is not disabled and the traces exporter is not "none". This is the rule
// of pkg/tracing.ConfigFromEnv, so local development needs no collector.
func (c Config) Exporting() bool {
	if c.SDKDisabled || strings.EqualFold(strings.TrimSpace(c.TracesExporter), "none") {
		return false
	}
	return strings.TrimSpace(c.Endpoint) != "" || strings.TrimSpace(c.TracesEndpoint) != ""
}

// tracesURL returns the URL the exporter sends spans to, with the rule of
// the OpenTelemetry specification: TracesEndpoint as given, or Endpoint with
// /v1/traces joined to its path.
func (c Config) tracesURL() (string, error) {
	raw, base := strings.TrimSpace(c.TracesEndpoint), false
	if raw == "" {
		raw, base = strings.TrimSpace(c.Endpoint), true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		field := "traces_endpoint"
		if base {
			field = "endpoint"
		}
		return "", fmt.Errorf("nanotrace: %s must be an http or https URL with a host", field)
	}
	if base {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/traces"
		u.RawPath = ""
	}
	return u.String(), nil
}

// Provider is the process tracer provider. It is a nano Stopper, which
// flushes the buffered spans and stops the exporter. It has no Start, so the
// App stops it whenever it was built: a build that fails after it, or a
// Starter that fails before the Installer, still flushes the spans of the
// failure. Installer publishes it globally.
type Provider struct {
	inner       *tracing.Provider
	logger      *slog.Logger
	sampleRatio float64
}

// NewProvider builds the tracer provider. identity supplies the fallback
// service.name and service.version; OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES win over them. The exporter does not connect here.
func NewProvider(ctx context.Context, cfg Config, identity log.Settings, logger *slog.Logger) (*Provider, error) {
	return newProvider(ctx, cfg, identity, logger)
}

func newProvider(
	ctx context.Context, cfg Config, identity log.Settings, logger *slog.Logger, opts ...tracing.Option,
) (*Provider, error) {
	if cfg.Exporting() {
		endpoint, err := cfg.tracesURL()
		if err != nil {
			return nil, err
		}
		opts = append([]tracing.Option{tracing.WithEndpointURL(endpoint)}, opts...)
	}
	inner, err := tracing.New(ctx, tracing.Config{
		Enabled:        cfg.Exporting(),
		SampleRatio:    cfg.SampleRatio,
		ServiceName:    strings.TrimSpace(identity.Service),
		ServiceVersion: serviceVersion(identity.Version),
	}, opts...)
	if err != nil {
		return nil, err
	}
	return &Provider{
		inner:       inner,
		logger:      logger.With(slog.String(log.KeyComponent, "tracing")),
		sampleRatio: cfg.SampleRatio,
	}, nil
}

// Stop flushes buffered spans and stops the exporter, bounded by ctx.
func (p *Provider) Stop(ctx context.Context) error {
	return p.inner.Shutdown(ctx)
}

// Installer publishes a Provider globally. It is a nano Starter, so the App
// builds the Provider even when no component takes it.
type Installer struct {
	provider *Provider
}

// NewInstaller returns the Installer of p.
func NewInstaller(p *Provider) *Installer { return &Installer{provider: p} }

// Start publishes the provider as the global TracerProvider with the W3C
// propagator, and sends OpenTelemetry errors to the log at Warn. Export
// errors never reach a request. Instrumentation created before Start, such
// as an HTTP middleware, delegates to the global provider and so uses it.
func (i *Installer) Start(ctx context.Context) error {
	p := i.provider
	p.inner.Install()
	logger := p.logger
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.WarnContext(context.Background(), "opentelemetry error", log.Err(err))
	}))
	p.logger.InfoContext(ctx, "tracer provider installed",
		slog.Bool("exporting", p.inner.Exporting()),
		slog.Float64("sample_ratio", p.sampleRatio),
	)
	return nil
}

// Exporting reports whether spans leave the process.
func (p *Provider) Exporting() bool { return p.inner.Exporting() }

// TracerProvider returns the provider to pass to instrumentation, such as
// pg/nanopg and HTTP middleware.
func TracerProvider(p *Provider) trace.TracerProvider {
	return p.inner.TracerProvider()
}

// ContextAttrs returns tracing.LogAttrs, which adds trace_id and span_id to
// every log line written with a context that carries a span. It is a member
// of the []log.ContextAttrs collection that core/logging consumes.
func ContextAttrs() log.ContextAttrs {
	return tracing.LogAttrs
}

// serviceVersion returns version, or the module version of the binary when
// version is empty, as pkg/tracing.ConfigFromEnv does.
func serviceVersion(version string) string {
	if version = strings.TrimSpace(version); version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}
