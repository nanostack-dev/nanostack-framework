//go:build nanogen

package collecterr

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("collecterr",
	nano.Providers(NewServer),
	nano.Collect[Route](NewHealthRoute, NewNotARoute),
)
