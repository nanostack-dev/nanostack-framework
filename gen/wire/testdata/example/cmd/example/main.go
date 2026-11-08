// Command example runs the example service until SIGINT or SIGTERM.
package main

import (
	"context"
	"os"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/gen/wire"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/greeter"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/example/internal/httpapi"
)

func main() {
	nano.Main(func(ctx context.Context) (*nano.App, error) {
		addr := os.Getenv("EXAMPLE_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		adminAddr := os.Getenv("EXAMPLE_ADMIN_ADDR")
		if adminAddr == "" {
			adminAddr = "127.0.0.1:9090"
		}
		cfg := example.Config{
			HTTP:     httpapi.Config{Addr: addr, AdminAddr: adminAddr},
			Greeting: greeter.Config{Greeting: "Hello"},
		}
		return wire.Build(ctx, cfg, wire.Overrides{})
	})
}
