//go:build nanogen

package clashtwo

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	billing "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashtwo/billing/repository"
	invoice "github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/clashtwo/invoice/repository"
)

var App = nano.Manifest("clashtwo",
	nano.Providers(
		invoice.New,
		billing.New,
		NewServer,
	),
)
