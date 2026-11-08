//go:build nanogen

package deadlock

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("deadlock", nano.Providers(NewStore))
