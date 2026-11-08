//go:build nanogen

package signatures

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/signatures/lib"
)

var App = nano.Manifest("signatures",
	lib.Module,
	nano.Providers(
		NewPool,       // (T, error), Stopper and HealthChecker
		OpenFile,      // (T, func(), error)
		NewRepo[User], // generic, (T)
		NewMemStore,   // (T)
		NewCache,      // Starter and Stopper
		NewSettings,   // value type
		NewServer,     // Runner
	),
	nano.Bind[Store, *MemStore](),
	nano.Invoke(Seed),
)
