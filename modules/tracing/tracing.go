// Package tracing wires pkg/tracing into an fx application: it builds the
// tracer provider from the standard OTEL_* environment, installs it and the W3C
// propagator globally, and flushes it on stop.
package tracing

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

// Module provides trace.TracerProvider and tracing.Config. Override the
// sampling or exporter decision with fx.Decorate on tracing.Config.
var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"tracing",
	fx.Provide(tracing.ConfigFromEnv, NewTracerProvider),
	// Forces construction, so the global provider and propagator are set
	// before any OnStart hook serves traffic, even when nothing injects
	// trace.TracerProvider directly.
	fx.Invoke(func(trace.TracerProvider) {}),
)

// Params are the dependencies of NewTracerProvider.
type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    zerolog.Logger
	Config    tracing.Config
	Options   []tracing.Option `group:"tracing.options"`
}

// NewTracerProvider builds the provider, installs it globally with the W3C
// propagator and registers its shutdown. Export errors are logged at warn and
// never surface to requests.
func NewTracerProvider(params Params) (trace.TracerProvider, error) {
	provider, err := tracing.New(context.Background(), params.Config, params.Options...)
	if err != nil {
		return nil, err
	}
	provider.Install()

	logger := params.Logger.With().Str("component", "tracing").Logger()
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn().Err(err).Msg("opentelemetry error")
	}))
	logger.Info().
		Bool("exporting", provider.Exporting()).
		Float64("sample_ratio", params.Config.SampleRatio).
		Msg("tracer provider installed")

	params.Lifecycle.Append(fx.Hook{OnStop: provider.Shutdown})
	return provider.TracerProvider(), nil
}
