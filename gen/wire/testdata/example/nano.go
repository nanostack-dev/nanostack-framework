//go:build nanogen

package example

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/greeter"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/httpapi"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/store"
)

var App = nano.Manifest("example",
	nano.Providers(
		NewLogger,
		greeter.NewService,
		httpapi.NewServer,
	),
	store.Module,
	nano.Bind[greeter.Store, *store.Memory](),
	nano.Invoke(Seed),
)
