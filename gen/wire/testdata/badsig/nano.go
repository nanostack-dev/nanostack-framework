//go:build nanogen

package badsig

import "github.com/nanostack-dev/nanostack-framework/core/nano"

var App = nano.Manifest("badsig",
	nano.Providers(
		NewTwoValues,
		NewVariadic,
		NewNothing,
		(*Thing).Method,
	),
	nano.Bind[Store, *Thing](),
	nano.Bind[*Thing, *Thing](),
	nano.Invoke(Returns),
)
