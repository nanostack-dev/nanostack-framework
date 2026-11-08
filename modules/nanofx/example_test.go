package nanofx_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
	"github.com/nanostack-dev/nanostack-framework/modules/nanofx"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

// A service still on fx replaces modules/pprof with the nano module: it
// supplies the typed Config (from its generated Bind in production), provides
// the nano constructor and registers the Runner with Lifecycle.
func ExampleLifecycle() {
	app := fx.New(
		fx.NopLogger,
		fx.Supply(nanopprof.Config{Enabled: false, Addr: "127.0.0.1:0", ShutdownTimeout: time.Second}),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Provide(nanopprof.NewServer),
		nanofx.Lifecycle[*nanopprof.Server]("pprof"),
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		panic(err)
	}
	fmt.Println(app.Stop(ctx))
	// Output: <nil>
}

// The dialing constructors take a context.Context first. Context provides
// it; in production it is the context that SIGTERM cancels. An empty
// nanocache.Config gives the no-op store, so this example needs no Redis.
func ExampleContext() {
	var store nanocache.Store
	app := fx.New(
		fx.NopLogger,
		nanofx.Context(context.Background()),
		fx.Supply(nanocache.Config{}),
		fx.Supply(slog.New(slog.DiscardHandler)),
		fx.Provide(nanocache.NewStore),
		nanofx.Lifecycle[nanocache.Store]("redis"),
		fx.Populate(&store),
	)
	if err := app.Err(); err != nil {
		panic(err)
	}
	fmt.Println(store.RedisClient() == nil)
	// Output: true
}
