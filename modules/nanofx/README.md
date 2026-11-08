# modules/nanofx

The bridge that runs nano components in an fx application, so a service moves from fx to generated wiring one module at a time.

`Lifecycle[T](name)` registers the lifecycle methods of the `T` in the fx graph, the way `nano.App.Manage` does:

- `Start` runs in `OnStart`.
- `Run` starts in a goroutine after `Start`. `OnStop` cancels it and waits for it. A `Run` that returns early shuts the application down with exit code 1.
- `Stop` runs in `OnStop`, after `Run` returned.
- `CheckHealth` is registered under `name` in the `*health.Registry`, when the graph has one.

`Context(ctx)` provides the `context.Context` that the dialing constructors take first (`nanopg.NewDB`, `nanocache.NewStore`, `nanolock.NewClient`). An fx graph has none of its own. Pass the context that the service's signal handling cancels.

```go
nanofx.Context(signalCtx),
fx.Supply(cfg.Postgres),
fx.Provide(nanopg.NewDB, nanopg.SQL, nanopg.NewTransactor),
nanofx.Lifecycle[*nanopg.DB]("postgres"),
```

The package is part of the legacy tree: it imports fx and is deleted with it. See [docs/modules.md](../../docs/modules.md).
