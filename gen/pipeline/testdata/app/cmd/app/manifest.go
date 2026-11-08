//go:build nanogen

package app

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("app", nano.Providers(NewGreeter))
