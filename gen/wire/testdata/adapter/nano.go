//go:build nanogen

package adapter

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("adapter",
	nano.Providers(
		NewServer, // Runner and Stopper
		AsHandler, // returns its *Server dependency as a Handler
	),
	nano.Invoke(Register),
)
