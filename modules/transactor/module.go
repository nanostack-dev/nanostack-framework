package transactor

import (
	dbtransactor "github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"

	"go.uber.org/fx"
)

// Module provides the shared SQL context transactor.
var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"transactor",
	fx.Provide(dbtransactor.New),
)
