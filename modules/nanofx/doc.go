// Package nanofx runs nano-shaped components in an fx application, so a
// service moves from fx to generated wiring one module at a time.
//
// A nano module provides plain constructors and gives its components the
// lifecycle methods of core/nano (Starter, Stopper, Runner, HealthChecker).
// In an fx graph, provide the constructors with fx.Provide, supply the typed
// configuration from the service's generated Bind, provide the context the
// constructors dial with through Context, and register the lifecycle with
// Lifecycle:
//
//	fx.New(
//		nanofx.Context(signalCtx),                   // cancelled by SIGTERM
//		fx.Supply(cfg.Postgres),                     // nanopg.Config from Bind
//		fx.Provide(nanopg.NewDB, nanopg.SQL, nanopg.NewTransactor),
//		nanofx.Lifecycle[*nanopg.DB]("postgres"),
//		...
//	)
//
// replaces postgres.Module and transactor.Module. When every module is
// nano-shaped, the service replaces fx.New with a manifest and nanostack gen wire.
//
// The package is part of the legacy tree: it imports fx and is deleted with
// it.
package nanofx
