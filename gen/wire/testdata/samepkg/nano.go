//go:build nanogen

package samepkg

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("samepkg",
	nano.Providers(newGreeter, newServer),
)
