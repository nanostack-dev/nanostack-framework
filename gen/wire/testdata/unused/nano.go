//go:build nanogen

package unused

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("unused",
	nano.Providers(
		NewServer,
		NewMetrics,
		NewMemStore,
	),
	nano.Bind[Store, *MemStore](),
	nano.Collect[Store](NewStoreFromServer),
)
