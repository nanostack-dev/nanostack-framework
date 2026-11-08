//go:build nanogen

package cycle

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("cycle",
	nano.Providers(
		NewA,
		NewB,
		NewC,
		NewServer,
	),
)
