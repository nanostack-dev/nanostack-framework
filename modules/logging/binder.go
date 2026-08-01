package logging

import (
	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

// EnricherGroup is the fx value group applications use to register their own
// log.Enrichers. A feature contributes what it owns without editing shared
// middleware:
//
//	fx.Provide(fx.Annotate(
//	    func() log.Enricher { return authEnricher{} },
//	    fx.ResultTags(`group:"log.enrichers"`),
//	))
const EnricherGroup = `group:"log.enrichers"`

// BinderParams collects everything needed to build the process-wide Binder.
type BinderParams struct {
	fx.In

	Logger    zerolog.Logger
	Config    Config
	Enrichers []log.Enricher `group:"log.enrichers"`
}

// NewBinder builds the Binder that log.Bind uses. The base logger carries the
// service identity from configuration, so every line — request-scoped or not —
// can be partitioned by service, environment and release without parsing the
// message.
func NewBinder(params BinderParams) *log.Binder {
	base := params.Logger.With()
	if params.Config.Service != "" {
		base = base.Str("service", params.Config.Service)
	}
	if params.Config.Environment != "" {
		base = base.Str("env", params.Config.Environment)
	}
	if params.Config.Version != "" {
		base = base.Str("version", params.Config.Version)
	}

	return log.NewBinder(base.Logger(), params.Enrichers...)
}

// InstallBinder publishes binder as the process-wide default, so package-level
// log.Ctx and log.Bind work from any call site without an injected dependency.
//
// It runs during fx construction rather than in an OnStart hook: components
// resolved later may log while they are still being built.
func InstallBinder(binder *log.Binder) {
	log.Install(binder)
}
