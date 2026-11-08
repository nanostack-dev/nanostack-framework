//go:build nanogen

package collect

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/auth"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/logging"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/middleware"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/syncs"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/workflow"
)

var App = nano.Manifest("collect",
	middleware.Module,
	auth.Module,
	workflow.Module,
	nano.Providers(
		syncs.NewSyncService,
		logging.NewEnriched,
	),
	nano.Collect[*workflow.Definition](syncs.ProvideSyncDefinition, NewBillingDefinition),
	nano.Invoke(UseLogger),
)
