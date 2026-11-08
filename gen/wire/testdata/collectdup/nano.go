//go:build nanogen

package collectdup

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("collectdup",
	nano.Providers(NewServer, NewRoutes),
	nano.Collect[Route](NewHealthRoute),
)
