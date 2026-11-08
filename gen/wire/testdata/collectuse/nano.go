//go:build nanogen

package collectuse

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("collectuse",
	nano.Providers(NewServer),
	nano.Collect[Route](NewHealthRoute),
)
