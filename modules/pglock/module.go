package pglock

import "go.uber.org/fx"

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"pglock",
	fx.Provide(ProvideConfig, NewClient),
)
