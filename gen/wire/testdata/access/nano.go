//go:build nanogen

package access

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("access",
	nano.Providers(
		newClient,
		NewServer,
	),
)
