//go:build nanogen

package valuerecv

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("valuerecv",
	nano.Providers(NewWorker),
	nano.Invoke(Use),
)
