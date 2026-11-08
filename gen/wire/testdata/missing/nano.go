//go:build nanogen

package missing

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("missing",
	nano.Providers(
		NewPGStore,
		NewService,
		NewServer,
	),
)
