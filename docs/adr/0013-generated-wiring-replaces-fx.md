# 0013: Generated wiring replaces fx

- Status: proposed
- Date: 2026-11-07
- Supersedes: ADR 0010 "Wiring is plain Go" from the open [PR #59](https://github.com/nanostack-dev/nanostack-framework/pull/59) (feature architecture), for how a service graph is assembled. PR #59 numbers its records 0002 to 0012, so these records start at 0013.

## Context

`go.uber.org/fx` v1.24.0 builds every service graph today. It appears in 19 files: `app/`, every lifecycle module under `modules/` (apisec, cache, config, httpserver, logging, migrations, pglock, postgres, pprof, tracing, transactor) and `pkg/fxlog`. `pkg/` is already free of it. Products assemble their services with `fx.Provide`, `fx.Invoke`, `fx.Replace` and `fx.Decorate`, and constructors take `fx.Lifecycle` to register start and stop hooks.

fx resolves the graph by reflection when the process boots:

- A missing or duplicate provider is a runtime error at boot, not a build error. A test that does not start the full graph never sees it.
- Test overrides (`fx.Replace`, `fx.Decorate`) are untyped. A replacement with the wrong type fails at boot.
- `fx.In`, `fx.Out`, `fx.Annotate` and named values carry the graph in struct tags and option values. Agents and reviewers read them poorly, and an editor cannot jump from a consumer to its provider.
- Lifecycle is a side effect of construction: a constructor that forgets `lc.Append` compiles and leaks.
- The last fx release is from May 2025, and the repository has had only chore commits since.

The 2026-05-22 "FX Module Adoption Slice" entry in the [decision log](../decision-log.md) adopted fx modules from the shared repository as one slice; this record replaces that wiring model, not that slice's reasoning about shared types.

## Decision

The framework generates plain Go wiring with `nano gen wire`, and a small runtime contract replaces fx.

- Providers stay plain constructors. A service lists them in a manifest (`nano.go`) that the generator reads with `go/packages` and `go/types`, the way a Wire injector lists provider sets, with modules as named provider groups.
- A provider may take its dependencies as parameters or as one `Deps` struct, the feature shape of PR #59. For a `Deps` struct the generator fills every field from the graph, so a field without a provider is a generation error.
- The generator writes ordinary Go: a `Build(ctx, cfg, Overrides) (*nano.App, error)` function that calls each constructor in dependency order. No reflection runs at boot.
- Lifecycle is detected from the provider's return type. A type that implements `nano.Starter`, `nano.Stopper`, `nano.Runner` or `nano.HealthChecker` is registered automatically. `nano.App` starts hooks in order, runs runners in an errgroup, and on a signal or the first runner error stops hooks in reverse with a timeout. The contract lives in `core/nano`, about 300 lines.
- Generation fails, with file and line and a suggested fix, on a missing provider, a duplicate provider, an ambiguous interface binding and a cycle (the cycle path is printed). `nano gen wire -check` in CI fails when the generated file is stale. (Implementation note, 2026-11-07: each generator runs as `nano gen <name> -check`; one `nano gen --check` that runs every generator in order is not built yet, see the [roadmap](../roadmap.md#deferred-generator-outputs).)
- Tests replace dependencies through a generated `Overrides` struct with one typed field per replaceable dependency. A nil field uses the production provider.
- The fx concepts map one to one: `fx.Provide` to the manifest, `fx.Lifecycle` hooks to the lifecycle interfaces, `fx.Replace` and `fx.Decorate` to `Overrides`, `fx.ValidateApp` to `nano gen wire -check`, `fx.Invoke` to `nano.Invoke`.
- The framework owns the manifest reader, lifecycle detection, `Overrides` and the output format. The graph solver is written in-house, unless a two-day timebox shows that the goforj/wire solver (Apache-2.0) can be embedded. Either way the output format is the framework's, so the solver can change without touching services.
- While products move, a temporary `nano.FxBridge` registers lifecycle components as fx hooks, so a product keeps compiling as its modules change. The bridge, `app/` and `pkg/fxlog` are deleted at the end of the migration.

## Alternatives

- **Keep fx.** Rejected. The failure modes above are inherent to resolving the graph by reflection at boot, not defects a release could fix.
- **google/wire.** Compile-time provider sets and a generated injector are the right model, but the project was archived in August 2025, and it has no lifecycle, health or typed override concept.
- **goforj/wire as the whole tool.** An active fork of Wire with caching and a watch mode. It has the same gaps as Wire for lifecycle, health and overrides. It is assessed only as the graph solver inside `nano gen wire`.
- **samber/do v2.** A generic runtime container. It types the lookups, but a missing dependency is still found at runtime.
- **go-kod/kod.** Generated components in the Service Weaver style. It imposes its own component model on every package, which the framework's plain-constructor `pkg/` code would have to adopt.
- **Hand-written wiring with struct literals and lint (PR #59, ADR 0010).** Each feature exposes `New(Deps)`, `internal/app` builds the features in order, and `exhaustruct` limited to `Deps` types (ADR 0012 there) fails a literal that leaves a field out. It is plain Go with no generator, and a service with a few features needs nothing more. It checks less than it appears to:
  - `exhaustruct` checks that every `Deps` field is written, not that the value is usable. A field set from a variable that is still nil, or from an override left nil, passes.
  - Order is checked only inside one function, by declaration order. Once `app.New` is split across files or helpers, nothing checks the order.
  - Lifecycle stays a side effect: a component whose `Stop` is never registered compiles and leaks, and stop order after a partial start is code each service gets right by hand.
  - Nothing reports an unused component, a duplicate one, or a health check that was never registered.
  - Each test override is a hand-written option on `app.New`, and ADR 0010 itself records that `app.New` becomes a merge hotspot as features grow.

  `nano gen wire` emits the same kind of plain Go `Build` function, so the output stays readable and debuggable, but it derives the order, the lifecycle registration and the typed `Overrides` from the manifest and checks the graph when it generates. A feature keeps its `New(Deps)` constructor as a provider, and the `exhaustruct` rule on `Deps` stays for the literals that are still written by hand: in tests, and in a product until it has a manifest.
- **Hand-written `main` without lint.** Plain and reflection-free, and fine for a tiny service. Every service would repeat ordering, shutdown and health registration by hand, and nothing would check the graph.

## Consequences

- The framework owns a generator of about 1.5k lines, with golden tests for its output and its error messages.
- Each `modules/*` constructor changes from taking `fx.Lifecycle` to returning a type that implements the lifecycle interfaces. This breaks the module API for products; the bridge keeps them building until they move to a manifest.
- A wiring mistake becomes a generation error with a location, and a test override becomes a compile-time type check.
- A `nofx` analyzer rejects new fx imports once the bridge is gone. The migration is done when `go.uber.org/fx` is absent from every `go.sum`.
