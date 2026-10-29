# 0010: Wiring is plain Go

- Status: accepted
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)
- Supersedes: the FX-based assembly in [app shell](../app-shell.md) and [boundaries](../boundaries.md)

## Context

Services wire themselves through Uber FX, and [`app/`](../app-shell.md) assembles a service from explicit `fx.Option` values.

- **The graph is resolved at runtime.** A missing provider or a cycle shows up when the service boots, not when it compiles. Echopoint documents a cycle only in a comment.
- **A diff doesn't show who receives a new dependency.**
- **Mechanism with no job.** FX also brings types that exist only to be provided, such as empty mapper structs.

[Boundaries](../boundaries.md) already keep every `pkg/` package free of FX, so services can build each piece with a plain constructor.

## Decision

- Each feature exposes `New(Deps) *Feature`, built with struct literals: no constructors and no container.
- `internal/app` builds the features in dependency order.
- `Run` uses `errgroup` and `signal.NotifyContext`.
- The framework offers a small lifecycle helper instead of FX modules.
- Every object is built once at startup and shared. Actions keep no request state, so `Execute` never writes its own fields.
- A `Deps` field left out of a struct literal still compiles and stays nil, so lint rejects it ([0012](0012-conventions-are-enforced-by-lint.md)).

```go
// feature/flows/flows.go
type Deps struct {
	DB        *sql.DB
	Tx        transactor.Transactor
	Reindex   SearchReindexer
	Variables EnvironmentResolver
}

type Feature struct {
	Handler *Handler
	Queries *Queries // exposed for other features' narrow interfaces
}

func New(d Deps) *Feature {
	flows, versions := &FlowRepository{db: d.DB}, &VersionRepository{db: d.DB}
	queries := &Queries{flows: flows, versions: versions}
	return &Feature{
		Queries: queries,
		Handler: &Handler{
			queries: queries,
			restore: &RestoreFlowVersion{flows: flows, versions: versions, reindex: d.Reindex, tx: d.Tx},
			publish: &PublishFlow{flows: flows, versions: versions, variables: d.Variables, tx: d.Tx},
		},
	}
}
```

```go
// internal/app/app.go: the order of this function is the dependency graph
search := resourcesearch.New(resourcesearch.Deps{DB: db, Queue: q})
vars := variables.New(variables.Deps{DB: db, Tx: tx})
flowsF := flows.New(flows.Deps{DB: db, Tx: tx, Reindex: search.Queue, Variables: vars.Resolver})

// Run: when one part exits, every other part stops
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return serveHTTP(ctx, a.HTTP) })
for _, w := range a.workers {
	g.Go(func() error { return w(ctx) })
}
return g.Wait()
```

Integration tests call `app.New` and read the fields they need, in place of `fx.Populate`. Options passed to `app.New` replace `fx.Decorate`.

## Alternatives

- **Uber FX everywhere (status quo).** It gives ordered lifecycle hooks, `fx.Populate` for tests and composable modules. Temporal uses it ([history/fx.go](https://github.com/temporalio/temporal/blob/main/service/history/fx.go)). It fits a binary that runs different component sets; ours each have one shape.
- **FX between features, plain Go inside.** `fx.Provide(flows.New)` with `Deps` as an `fx.In` struct. This is the intermediate step while migrating.
- **Google Wire.** It generates the hand-written wiring, so errors are caught at compile time, at the cost of a generator and provider sets. Grafana uses it ([pkg/server](https://github.com/grafana/grafana/tree/main/pkg/server)). Choose it if `app.New` outgrows a reviewer's attention.
- **Global `Init()` functions** ([Gitea](https://github.com/go-gitea/gitea/blob/main/routers/init.go)). Rejected: hidden global state breaks parallel tests ([Google](https://google.github.io/styleguide/go/best-practices#global-state)).

## Consequences

- Building a feature before what it needs is a compile error, because Go rejects a variable used before its declaration. A missing `Deps` field fails lint, and a cycle cannot be written without someone breaking it on purpose. This follows "make dependencies explicit" ([Peter Bourgon](https://peter.bourgon.org/go-best-practices-2016/)) and Mat Ryer's `run(ctx, …)` ([Grafana blog](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/)). Prometheus runs its whole lifecycle in one run group ([main.go](https://github.com/prometheus/prometheus/blob/main/cmd/prometheus/main.go)).
- Stopping in reverse order after a partial startup is written once, in the framework helper.
- Cycles break by building every feature's `Queries` first, since they need only the database. A write-to-write cycle goes through the outbox ([0007](0007-side-effects-leave-through-the-outbox.md)).
- `app.New` grows with each feature and is a merge hotspot. CockroachDB's `NewServer` is 1,231 lines ([server.go](https://github.com/cockroachdb/cockroach/blob/HEAD/pkg/server/server.go#L256)). Each test override needs an explicit option.
- `modules/` loses its FX role. This supersedes the FX-based assembly described in [app shell](../app-shell.md) and [boundaries](../boundaries.md). It applies to every service (OQ-1), so services keep one way of starting.
