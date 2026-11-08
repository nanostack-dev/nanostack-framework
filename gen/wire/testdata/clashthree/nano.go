//go:build nanogen

package clashthree

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/cache"
	eu "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/eu/billing/store"
	orders "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/orders/store"
	us "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashthree/us/billing/store"
)

var App = nano.Manifest("clashthree",
	nano.Providers(
		us.NewLedger,
		orders.NewLedger,
		cache.NewLedger,
		eu.NewLedger,
		NewServer,
	),
)
