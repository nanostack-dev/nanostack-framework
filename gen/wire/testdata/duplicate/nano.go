//go:build nanogen

package duplicate

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("duplicate",
	nano.Providers(
		NewStore,
		NewOtherStore,
		NewDBConfig,
		NewServer,
	),
)
