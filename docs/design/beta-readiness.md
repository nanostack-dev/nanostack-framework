# Beta readiness: design

- Status: proposed
- Date: 2026-11-08
- Branch: `feat/framework-beta`, stacked on `feat/db-fork`, which is stacked on `feat/framework-core` (PR #64)
- Inputs: overnight log decisions 9 and 10 (`reports/overnight-2026-11-08.md`), the migration scans (`reports/migration/{echopoint,anchor,critic}.md`), the framework plan (§06, §09, §19, §20, §23), ADRs 0013 to 0022, `docs/decision-log.md`, `AGENTS.md`, `CONTEXT.md`.

This document decides the work that must land before the framework is tagged as a beta, so that echopoint (pins v0.22.0) and anchor (pins v0.20.0) can migrate without a change to their wire contract or their alerts. Each decision has its reasons and the alternatives that were rejected. Each one was judged on end-state quality ("would we pick this from scratch?"), not on the size of the change.

Paths without a repository prefix are relative to this worktree. `otelcol.yaml`, `alert-rules.yaml` and `dashboards/` are in the `infra` repository at main `40b7988` (`stacks/`, `stacks/grafana/`). `EP` is the echopoint scan, `AN` the anchor scan and `CR` the critic scan, all under `reports/migration/`. Upstream go-jet commits come from the mirror that `db/tools/upstreamsync` keeps (`~/Library/Caches/nanostack/db-upstream/go-jet.git`), range `v2.15.0..d0e152c` (40 commits).

## Decisions at a glance

| # | Decision |
|---|---|
| A1 | Port `pkg/db/transactor`, `pkg/jetx` and the `pkg/testkit/searchtest` integration test to Nanostack DB **in place**. Their import paths do not change. A consumer changes only its own go-jet imports. |
| A2 | `pkg/search` and `pkg/db/pgerr` stay where they are. The move into the `db` module waits for the executor stage of ADR 0015, where the API changes anyway. |
| B1 | Nano modules go in new integration-layer trees: `pg/`, `redis/`, `otel/`, plus `http/nano*` and `core/logging`. The layer test ranks `pg`, `redis` and `otel` as layer 2, next to `http`. |
| B2 | Integration packages may import a closed allow-list of `pkg/` primitives that have no fx and no zerolog in their dependencies. This is a listed exception that ends when each primitive moves to its layered home. `core/` stays strict. |
| B3 | Each capability has one implementation. The deprecated fx module becomes a thin wrapper over the nano providers, and keeps its fx signatures for the beta. **Slips to beta.2** (decision log, 2026-11-08): beta.1 ships two implementations, and CHANGELOG.md lists their differences as a known risk. |
| B4 | `modules/nanofx` (fx bridge) lets a service move one module at a time while it still runs on fx. |
| C1 | `core/log` gets `Config.Wire = WireLegacy`. This writes lowercase `level`, `message`, a flat `caller` and durations in milliseconds, so a slog line is wire-identical to a zerolog line. |
| C2 | `log.HTTPOptions.LegacySummary` makes the request summary match `pkg/httputil/requestlog`: Info for a cancelled request, and `query` as one redacted string. Request bodies are never logged. |
| C3 | The envelope seam is the three error hooks that already exist (strict request and response handlers, parameter binding) plus the router's 404 and 405. A slog renderer in `pkg/httputil/errorenvelope` writes the legacy `{"errors":[...]}` body. `modules/httpserver.StrictErrorHandler` uses the same resolution. |
| C3 | The framework does not reproduce kin-openapi text. A service that adopts generated validation first updates every gate assertion on that text, as AGENTS.md requires. For echopoint these are 24 assertions in 6 flows: 7 fail with the C3 message format and 17 pass with it by chance (C3, "kin-openapi text"). |
| C4 | `nanohttp.NewRouter` wraps a ServeMux. It takes NotFound and MethodNotAllowed handlers and has an `ExplicitHEAD` option for chi parity. |
| C5 | `nano.AppConfig` gets `config:` tags with defaults (15s, 15s, 5s). A service binds it as `APP_*`. The default stop timeout stays 15s, and the migration guide requires `stop_grace_period`. |
| C6 | `nanohttp` decoding gets no lenient mode. A service that adopts it lists the strictness changes (413, 415, case, duplicates) in its PR. |
| D1 | Tags: `pkg/functional/vX.Y.Z`, `db/vX.Y.Z`, root `vX.Y.Z`. Versions are independent (ADR 0014). The first beta is `v0.23.0-beta.1` for all three. |
| D2 | **Amended (decision log, "Development Form and Release Branches").** `main` keeps the development form: intra-repository `v0.0.0` with a `replace` to the directory. A tagged `go.mod` has no `replace`, no `v0.0.0` and no pseudo-version. A release is staged leaves first on a `release/vX.Y.Z` branch: functional, then db, then root. |
| E | T1 = A + D, T2 = C, T3 = B. File ownership is disjoint. T3 starts against the T1 and T2 contracts in section E2. |

## Context: what blocks the migrations

- Every framework module is fx-only. Eleven modules import fx, and none imports `core/nano` (CR A10, EP §2 "Blockers" item 1). Echopoint uses 11 of them and anchor uses 9 (AN §2).
- `pkg/db/transactor` and `pkg/jetx` import upstream go-jet, and a service on the fork would link two incompatible jet type systems (EP §6 "Constraint", AN §6 "Blocker", CR B6).
- `core/log` output breaks the shared log pipeline. It writes uppercase `level` (`core/log/config.go:162`) and `duration` in nanoseconds (`core/log/http.go:163`). The alerts read lowercase severity (`infra/stacks/grafana/alert-rules.yaml:319,403`) and `duration:>1000` in milliseconds (`alert-rules.yaml:362`).
- `http/problem` always writes `application/problem+json` (`http/problem/write.go:17,84`). The gates assert error codes 297 times (anchor) and 322 times (echopoint), message text 48 times in echopoint, `Content-Type: application/json` 18 times on anchor 401s, and 405 five times on echopoint `/health` (CR B1).
- `nano.DefaultStartTimeout` and `DefaultStopTimeout` are 15s (`core/nano/app.go:17-18`). `AppConfig` has only `json`/`yaml` tags (`core/nano/app.go:26-32`), so `nano gen config` cannot bind it.
- The root `go.mod` requires `pkg/functional v0.0.0` with a local `replace` (`go.mod:23,117`), and `db/go.mod` does the same (`db/go.mod:11,26`). Consumers ignore `replace`, so `go get` of such a tag fails (overnight decision 8).

---

## A. Database helpers on Nanostack DB

### A1. Decision

T1 ports these packages to `github.com/nanostack-dev/nanostack-framework/db/...` and keeps their paths:

| Package | go-jet imports today | After |
|---|---|---|
| `pkg/db/transactor` | `jet/v2/postgres`, `jet/v2/qrm` (`transactor.go:8-9`, `row_lock.go`, `page.go`, `keyset.go`, tests) | `db/postgres`, `db/qrm` |
| `pkg/jetx` | `jet/v2/postgres` (`jetx.go`, `jetx_test.go`) | `db/postgres` |
| `pkg/testkit/searchtest` | only the integration test (`searchtest_integration_test.go:19`, tag `integration`) | `db/postgres` |
| `pkg/search`, `pkg/db/pgerr` | none (`go list` shows no jet import; CR A11) | unchanged |

The root module drops `github.com/go-jet/jet/v2` from `go.mod` and requires `github.com/nanostack-dev/nanostack-framework/db`. No exported signature changes, except that its jet and qrm types now come from the fork.

### A2. Reasons

1. **The end-state home has a different API.** ADR 0015 moves the transactor into `db/exec`, which defines an `Executor` over an abstract `Rows`, with pgx (`docs/adr/0015-fork-go-jet-as-the-db-module.md`, "Decision"). A move now to `db/transactor` with today's `qrm.DB` API is not that end state. Consumers would change imports twice: once now, and again at the executor stage.
2. **One query entry point.** The decision log makes the transactor the only way to run a statement (`docs/decision-log.md`, 2026-07-27 "Single Query Entry Point"). `db` already holds `db/query` (`One`, `All`, `Exec`, `Count`; `db/query/query.go:36-106`) and `db/filter` (`db/filter/filter.go:29-55`). If the transactor moved next to them, `db` would export two query APIs. The executor stage merges them. Until then, services use the transactor, and `db/query` stays a dialect-neutral helper for code without a transactor.
3. **The `db` module is dialect-neutral.** The transactor is PostgreSQL-only: it builds on `db/postgres` and maps constraint errors through `pkg/db/pgerr`, which binds `lib/pq`. Note that `db/go.mod` requires `lib/pq`, `go-sql-driver/mysql` and `mattn/go-sqlite3` for its generator (`db/go.mod:5-12`). So the module is not driver-free yet. But its runtime packages are dialect packages, and a PostgreSQL error mapper does not belong among them.
4. **No module cycle.** The transactor imports `pkg/search`, `pkg/jetx` and `pkg/db/pgerr` (`go list -f '{{.Imports}}' ./pkg/db/transactor`). In `db`, it would need all three moved too, or `db` would require the root module, which requires `db`.

### A3. Rejected alternatives

- **Move to `db/transactor`, `db/jetx`, `db/search` now** (the plan's §06 module catalogue). Rejected for reasons 1 to 4. It also changes 97 echopoint `pkg/search` import sites and anchor's 110 (EP §1 table, AN §6) for no change in behaviour.
- **Copy into `db` and keep generic type aliases at the old paths.** ADR 0014 allows aliases to ease a move. Rejected for now, because the destination API is not final. Aliases are the tool for the executor stage.
- **Keep upstream go-jet in `pkg/` and add fork-based twins** (for example `pkg/db/transactor/v2`). Rejected: one service would link two jet type systems, which is the blocker this work removes (EP §6).

### A4. Port steps (T1)

1. Rewrite the imports in the three packages. Then `go mod tidy` in the root module. `grep -rn 'github.com/go-jet/jet/v2' --include='*.go' pkg` must print nothing.
2. Run the unit tests, and the integration tests with `-tags integration` and a PostgreSQL DSN (`pkg/db/transactor/*_integration_test.go`, `pkg/testkit/searchtest/searchtest_integration_test.go`).
3. Add a test that pins row-lock SQL. `transactor/row_lock.go:69` calls `selectStmt.FOR(lock)`. In the fork, `FOR` is variadic and replaces the lock list (upstream `afea45b`, `postgres/select_statement.go`), so one call writes one `FOR` clause as before. The test asserts the SQL text of `ForUpdate`, `ForShare`, `ForNoKeyUpdate` and `ForKeyShare`.
4. Regenerate echopoint's and anchor's models in scratch copies (never in their repositories) with the fork's generator, and diff them against today's output. Record the diff in the T1 report. Expect import paths only, unless a model customizes a field name (see A5).

### A5. go-jet v2.15.0 to fork (v2.16.1 + #628): what consumers see

Upstream changes in `v2.15.0..d0e152c`:

| Change | Commit | Consumer impact |
|---|---|---|
| Raw SQL with named arguments where one name is a prefix of another (`:id`, `:id_2`) now serializes correctly | `3b7f200` (#628), `internal/jet/sql_builder.go` | **SQL can change.** Echopoint uses `postgres.Raw` 17 times and `.Raw(` 36 times (EP §6). Review every `Raw` call with overlapping argument names. Before, such a call could bind the wrong value. |
| Type wrappers (`IntExp`, `StringExp`, ...) no longer mutate the wrapped expression | `886bc04` (#623), 14 `internal/jet/*_expression.go` files | Code that reused an expression after wrapping it got a changed expression before. Now it does not. Low risk, but SQL can change. |
| `FOR(locks ...RowLock)` is variadic. `ClauseFor.Lock` is now `Locks`. SQLite loses its unsupported `FOR`/`LOCK` | `afea45b` (#616) | Call sites compile unchanged. A type that implements `SelectStatement` itself breaks (none known). |
| The generator adds a `column:"<name>"` tag when a customized field name differs from the default. `MODEL`/`INSERT` find a field by that tag | `5c68336` (#596), `generator/template/model_template.go`, `internal/utils/dbidentifier/dbidentifier.go:24-43` | Generated models change **only** where a template renames a field. Echopoint's hook changes a type (`text[]` to `pq.StringArray`, `dbgen.go:18-22`), not a name. Anchor's does the same (`dbgen.go:35-37`). Before, a hand-written `MODEL` struct with a non-standard field name panicked. Now it works when the field has a `column` tag. |
| `MATERIALIZED` CTEs, `WITH` as an `INSERT` source, chained operators on window expressions, exported `SetStatement` | `3c5e0a9`, `dc239b9`, `4d071ae`, `18b39c5` | Additive. |
| Shell wildcards in the CLI `-ignore-*` filters | `ca6185f`, `cmd/jet/main.go` | CLI only. Both services generate through the Go API (`dbgen.go`), so nothing changes for them. |
| MySQL escapes backslashes in quoted literals | `24caf8d` (#621) | MySQL only. Both services use PostgreSQL. |
| Test helpers moved out of runtime packages | `94dda0f` | None exported that consumers use. |

Fork changes (`db/UPSTREAM.md`, "Changes from upstream"):

| Change | Consumer impact |
|---|---|
| Module path `github.com/nanostack-dev/nanostack-framework/db` | The mechanical import rewrite. |
| qrm fills `functional.Option[T]` fields like `*T`. `*Option[T]`, `Option[*S]`, nested Options and a top-level Option destination panic with a fix | **Grep first.** A consumer that scans into a struct with `functional.Option` fields through its own `Scan` method gets the qrm conversion instead. Search models and `QRM` destinations for `functional.Option` before the swap. |
| An Option argument unwraps: absent is NULL, present is the value. This applies to `MODEL`, `VALUES`, `SET`, `Literal`, `Raw`. None in `MODEL` is NULL, not DEFAULT | A `MODEL` struct with an Option field now inserts NULL for None. Before, the driver got the Option value. |
| `XxxOpt` literal constructors, `-nullable=pointer|option` generator flag (default `pointer`) | Additive. The default keeps `*T`. |
| New packages `query`, `filter`, `stmtcache` | Additive. Services keep the transactor (A2, reason 2). |
| `jet -version` prints the Nanostack version | Additive. |
| Generated files keep `// Code generated by go-jet DO NOT EDIT.` | No change in headers. |

Do not promise "import paths only". T1's model diff (A4, step 4) is the evidence for each service.

### A6. Consumer migration (mechanical)

1. `go get github.com/nanostack-dev/nanostack-framework@v0.23.0-beta.1`, then `go mod tidy` (D6).
2. Rewrite imports: `github.com/go-jet/jet/v2/<pkg>` to `github.com/nanostack-dev/nanostack-framework/db/<pkg>` (for example with `gofmt -r` or `sed`). The framework helper paths (`pkg/db/transactor`, `pkg/jetx`, `pkg/search`) do not change.
3. Change `dbgen.go` to import `.../db/generator/postgres` and `.../db/generator/template`, regenerate, and review the diff against A5.
4. Check `Raw` calls with overlapping argument names, and `functional.Option` fields in scan destinations and `MODEL` structs.
5. Run the repository and integration suites (echopoint `cmd/it/repository` runs in no CI check, AN/CR A14: run it by hand).

---

## B. Nano modules

### B1. Where nano modules live

**Decision.** New integration-layer trees, as plan §06 names them: `pg/`, `redis/`, `otel/`. HTTP modules go in new packages under `http/`. The logging module goes in `core/logging`.

`internal/layers.Layer` returns 0 for any top-level directory other than `core`, `http`, `gen` and `cmd` (`internal/layers/layers.go:26-37`). So new trees would be unchecked, not allowed. T3 extends the rank map: `pg`, `redis` and `otel` are layer 2 ("integrations"), with `http`. The depguard rules in `.golangci.local.yml:24-60` get the same entries.

**Why `core/logging` and not `core/log`.** A module is a package-level `nano.Module` value. `core/nano` imports `core/log` (`core/nano/app.go:12`), so a module value in `core/log` would make an import cycle. The separate package is forced.

**The `pkg/` allow-list (exception to the AGENTS.md invariant).** AGENTS.md says no layered package imports `pkg/`, `modules/` or `app/`. The reason is that those trees "carry Fx and zerolog" (`internal/layers/layers.go:9-11`). Some nano modules need primitives that live in `pkg/` and carry neither:

| Package | fx or zerolog in `go list -deps` | Used by |
|---|---|---|
| `pkg/db/transactor` | 0 | `pg/nanopg` |
| `pkg/db/pgerr` | 0 | (through the transactor) |
| `pkg/jetx` | 0 | (through the transactor) |
| `pkg/search` | 0 | (through the transactor) |
| `pkg/tracing` | 0 | `otel/nanotrace` |
| `pkg/apisec` | 0 | `http/nanoapisec` |
| `pkg/health` | 0 | `http/nanoserver` (mounts `/health`) |

(`pkg/redisx` has 2 and is not on the list.)

Rule: a layer-2 package (`http`, `pg`, `redis`, `otel`) may import these packages, and only these. `core/` may import none. The layers test checks that each allow-listed package still has no fx and no zerolog in its dependencies, so a later change cannot bring them in. AGENTS.md records the exception and its end: each entry leaves the list when its package moves to its layered home (transactor to `db/exec`, tracing to `otel`, apisec to `http`, health to `core`).

**Rejected alternatives.**

- **Copy the needed code into the layered trees now.** This duplicates the transactor, the tracing provider and the security resolver, and consumers would hold two types for one concept during the migration.
- **Put nano modules in `modules/<name>/nano` (legacy tree).** New wiring code would then sit in a tree that the layer rule bans and that is deleted later. Every service would change those imports again.
- **Put the nano `Module` value in the primitive's own `pkg/` package** (for example `transactor.Module`). It works for pure providers, but new lifecycle code would live in the legacy tree, against AGENTS.md ("New framework code goes in the layered trees").

### B2. Module contract

Every nano module follows one shape, so that services migrate the same way for each one:

1. **Config.** A struct in the module package. Every field has `config:` tags (and `default:` where the fx module had a default), **and** `yaml:` tags with the legacy key (`dbname`, `tableName`, `basePath`, `autoFixDirty`, ...). The deprecated fx module fills the same struct through `config.Loader.LoadConfig`, so the fx module and the nano module share one implementation. Secret fields are `secret.Secret`. `Secret.UnmarshalText` (`core/secret/secret.go:90-93`) lets the legacy YAML loader fill them too.
2. **No `env:` tags in module configs**, except for process-global names. The service chooses the section name, and `core/config` derives the env name from the path. For example, section `postgres` plus field `db` gives `POSTGRES_DB`, and section `redis` plus field `address` gives `REDIS_ADDRESS`. Fixed env tags in a library would collide when a service has two sections of one type (`gen/config/load.go:277-283` rejects a duplicate env).
3. **Providers** are plain functions shaped `func(deps...) T` or `(T, error)` (`core/nano/manifest.go:27-29`). They take `*slog.Logger`, never zerolog.
4. **Lifecycle** through the method shape of the returned type: `Starter`, `Stopper`, `Runner`, `HealthChecker` (`core/nano/lifecycle.go:25-49`). The interfaces are structural, so a type outside the framework (pgkit) satisfies them without importing `core/nano`.
5. **`var Module = nano.Module("<name>", nano.Providers(...))`** in the package. The service's manifest lists it.
6. **README, doc.go, an example test**, as plan §19 requires of every module.

### B3. Per-module decisions

**Status in beta.1.** The thin wrappers slip to beta.2 (decision log, 2026-11-08 "Beta Readiness Review Fixes"). The last column says which fx modules are separate implementations in beta.1. Only `modules/apisec` is a wrapper. `CHANGELOG.md` lists the differences as a known risk.

| fx module (today) | Nano package | Providers | Config (service section, env derived) | Lifecycle | fx module in the beta |
|---|---|---|---|---|---|
| `modules/config` (`configloader.go`, `${VAR}` interpolation) | none: `core/config` + `nano gen config` | the generated `Bind`. The generator provides the manifest package's `Config` and each exported field (`core/nano/manifest.go:27-31`) | the service's own `Config` struct | none | Kept, deprecated. No nano equivalent is needed. |
| `modules/logging` (`logging.go:27-110`, `binder.go`) | `core/logging` | `NewLogger(s log.Settings, attrs []log.ContextAttrs) *slog.Logger`. Under nano it calls `slog.SetDefault`, as the fx `InstallBinder` installs its binder at construction (`binder.go:51-53`), because `http/problem.Write` logs on the default logger (`http/problem/write.go:21`). `nano.Collect[log.ContextAttrs]()` declares the (possibly empty) collection | `log.Settings` with process-global env tags that match `log.ConfigFromEnv` (`core/log/config.go:59-72`): `LOG_LEVEL`, `LOG_FORMAT`, `LOG_WIRE`, `ENVIRONMENT`, `SERVICE_NAME`, `SERVICE_VERSION` | none | Kept, deprecated. It also provides a `WireLegacy` `*slog.Logger` to the fx graph, and does **not** call `slog.SetDefault` (B5). `EnricherGroup` has no nano form: enrichment is middleware that calls `log.SetAttrs` (`core/log/context.go:66`). |
| `modules/tracing` (`tracing.go:19-58`) | `otel/nanotrace` | `NewProvider(ctx, Config, *slog.Logger) (*Provider, error)` installs the global provider and propagator, as today. `TracerProvider(*Provider) trace.TracerProvider`. `ContextAttrs() log.ContextAttrs` returns `tracing.LogAttrs` and is listed as a member of `nano.Collect[log.ContextAttrs](nanotrace.ContextAttrs)`. A member is a provider whose result is assignable to the element type (`core/nano/manifest.go:69-87`), so a function of the element's own shape cannot be listed directly. Then `core/logging` adds `trace_id`/`span_id` | `Config` keeps the `OTEL_*` names (`pkg/tracing/config.go:20-24`), which are a standard contract, through `env:` tags | `Stopper`: flush and shut down | Kept, deprecated. Separate implementation until beta.2 (B3 slip). |
| `modules/postgres` (`postgres.go:28-117`) | `pg/nanopg` | `NewDB(ctx, Config, *slog.Logger, trace.TracerProvider) (*DB, error)` opens `otelsql` + `lib/pq` with today's trace options and pool (25/25/5m, AN §6) and pings. `SQL(*DB) *sql.DB` | section `postgres`: `host`, `port`, `user`, `password` (secret), `db` (yaml `dbname`, env `POSTGRES_DB`), `sslmode`, `max_open_conns` (25), `max_idle_conns` (25), `conn_max_lifetime` (5m) | `Stopper` (Close), `HealthChecker` (Ping, name `postgres`) | Kept, deprecated. Separate implementation until beta.2 (B3 slip). |
| `modules/transactor` (`module.go:10-12`) | `pg/nanopg` (same module) | `NewTransactor(*sql.DB) transactor.Transactor` | none | none | Kept, deprecated. |
| `modules/migrations` (`module.go:13-43`, `migrator.go`) | `pg/nanomigrate` | `NewMigrator(Config, *sql.DB, *slog.Logger) *Migrator`, and `Run(ctx, *Migrator) error` listed with **`nano.Invoke`** | section `migrations`: `enabled` (true), `base_path` (yaml `basePath`, `migrations`), `validate` (true), `auto_fix_dirty` (yaml `autoFixDirty`, false), `force_version` | none (see B4) | Kept, deprecated. Separate implementation until beta.2 (B3 slip). |
| `modules/pglock` (`pglock.go:22-70`) | `pg/nanolock` | `NewClient(ctx, Config, *sql.DB, *slog.Logger) (*pglock.Client, error)`. It runs the startup test when `test_on_startup` is set, as today (`pglock.go:48`) | section `pglock`: the defaults of `ProvideConfig` (`pglock.go:61-69`) as `default:` tags, yaml keys `tableName`, `autoCreateTable`, `leaseDuration`, `heartbeatFrequency`, `testOnStartup` | none (the fx stop hook is a no-op, `pglock.go:57`) | Kept, deprecated. Separate implementation until beta.2 (B3 slip). |
| `modules/cache` (`module.go:10-20`, `cache.go`, `redis.go`, `noop.go`) | `redis/nanocache` | `NewStore(ctx, Config, *slog.Logger) (Store, error)`: Redis when `address` is set, otherwise the no-op store, as the fx module does. `New[T](Store, prefix, ttl, *slog.Logger) (Cache[T], error)` (slog form of `cache.go:40`; the error rejects an empty prefix and a prefix with a glob character or a trailing `:`, beta audit recheck) | section chosen by the service. Echopoint picks `redis` to keep `REDIS_ADDRESS`, `REDIS_PASSWORD` (secret), `REDIS_DB` (EP §3) | `Stopper` (Close), `HealthChecker` (Ping, Redis only) | Kept, deprecated. `modules/cache.Store` becomes a type alias of `nanocache.Store`, so fx and nano code share one store during the move. `modules/cache.Cache[T]` keeps its zerolog signature until removal. |
| `modules/httpserver` (`module.go:35-150`) | `http/nanoserver` | `NewServer(Config, http.Handler, *slog.Logger) *Server`. The service provides the `http.Handler`: it keeps its router (chi for the beta), its middleware order and its `/health`. The fx module's chi setup stays in the fx module | section chosen by the service (`http`): `port` (8080, `module.go:26`), `read_header_timeout`, `read_timeout`, `write_timeout`, `idle_timeout`, `shutdown_timeout`, same defaults as `withDefaults` (`module.go:57-90`) | `Runner`: `ListenAndServe`, then `Shutdown` when its context ends. It replaces the services' own goroutine, signal handler and `Fatal` (EP §2 item 4, AN §2 "Shutdown quirks") | Kept, deprecated. `StrictErrorHandler` uses the shared resolution (C3). |
| `modules/apisec` (`module.go:16-30`) | `http/nanoapisec` | `NewResolver(Document) (*apisec.Resolver, error)`. The service provides `nanoapisec.Document` from a provider that returns its embedded spec (replaces `fx.Supply`, EP §2, AN §2) | none | none | Kept, deprecated, thin wrapper (done in beta.1). |
| `modules/pprof` (`module.go:22-110`) | `http/nanopprof` | `NewServer(Config, *slog.Logger) *Server` | section `pprof`: `enabled` (env tag `ENABLE_PPROF`, the name both services and infra use, `module.go:18`; CR B8), `addr` (`127.0.0.1:6060`, `module.go:17`), the timeouts of `withDefaults` (`module.go:32-54`) | `Runner` when enabled. When disabled, `Run` blocks until its context ends and serves nothing | Kept, deprecated. Separate implementation until beta.2 (B3 slip). |
| `modules/pgqueue` (README only, `modules/pgqueue/README.md`) | none in the framework | pgkit owns its queue, workflow and admin UI lifecycle (`pgkit/fx/{queue,workflow,adminui}`, EP §2 "Blockers" item 2). Its types implement `Start`/`Stop`/`Run` by method shape and need no framework import | n/a | pgkit types | The README is rewritten to say this and to point at pgkit. |

`app/` (the fx builder) and `pkg/fxlog` are kept and deprecated for the beta. Removal of every fx module is a later major step (ADR 0013).

### B4. Per-module notes

- **`/health` stays a contract.** It answers `{status, service, environment, version, commit_sha, build_date?}` (`pkg/health/health.go:94-100`, EP §5). It is the Docker `HEALTHCHECK` (EP §5), and the gate asserts 405 on it five times (CR B1). `/livez` and `/readyz` from `App.AdminHandler` (`core/nano/admin.go:29`) do not replace it in the beta. `http/nanoserver` provides `HealthHandler(health.Config, *nano.App) http.Handler`, which serves the legacy body and adds the nano readiness checks as dependencies. The service mounts it on its own router.
- **Migrations run in `nano.Invoke`, not in a `Starter`.** The fx module runs `RunBeforeStart` in `fx.Invoke` on purpose: construction has no deadline, while `OnStart` hooks do (`modules/migrations/module.go:24-29`). Two tests pin this behaviour (`migrations_integration_test.go:94`, slower than the start timeout and ends clean; `:117`, a shutdown signal finishes the migration in progress). `nano.Invoke` runs inside the generated `Build` with `Main`'s signal context (`core/nano/main.go:38-40`), so it keeps both behaviours. A `Starter` would be bounded by `StartTimeout` (15s), and a long backfill would fail the start, after which Swarm rolls back (CR B3). T3 ports both tests to `pg/nanomigrate`.
- **Logging and tracing join through `nano.Collect`**, not through a value group. `log.ContextAttrs` is the element type. This is the pattern the decision log chose for listed members (`docs/decision-log.md`, 2026-11-07 "Generated Wiring Replaces Fx").
- **No module reads `os.Getenv` directly.** The env switches named in CR B8 keep their names through the config tags above (`ENABLE_PPROF`, `POSTGRES_DB`, `REDIS_*`, `ENVIRONMENT`). The service still owns `ECHOPOINT_*`, `PGKIT_DASHBOARD_*` and the duration overrides.
- **`*_FILE` secrets.** Services keep `runtimeenv.HydrateFileBackedEnv()` before `Bind` (EP §3, recommendation (a)). A `_FILE` `SecretProvider` in `core/config` is out of scope for the beta.

### B5. Moving fx to nano one module at a time

**Decision.** `modules/nanofx` (legacy tree; it imports fx) bridges nano-shaped components into an fx graph:

```go
// Lifecycle registers the lifecycle methods of the T in the fx graph:
// Start as OnStart, Stop as OnStop, Run as a goroutine that OnStop cancels
// and waits for, CheckHealth in *health.Registry when one is provided.
func Lifecycle[T any](name string) fx.Option
```

A service on fx swaps one module at a time, for example `postgres.Module` for `nanofx.Context(signalCtx), fx.Provide(nanopg.NewDB, nanopg.SQL, nanopg.NewTransactor), nanofx.Lifecycle[*nanopg.DB]("postgres")`, and supplies the typed config from its generated `Bind` (`fx.Supply(cfg.Postgres)`). `nanofx.Context` provides the `context.Context` that the dialing constructors take first. When every module is nano-shaped, it replaces `fx.New` with a manifest and `nano gen wire`. So typed config (`core/config`) comes before the module moves. EP §10 already orders the config step before the wiring step.

The deprecated fx modules keep their fx signatures (`zerolog.Logger`, `config.Loader`) for the beta, so a v0.22.0 consumer upgrades with no graph change. Internally they call the nano providers (beta.2; in beta.1 only `modules/apisec` does, see B3). They take `*slog.Logger` from the fx graph, where `modules/logging` provides a `WireLegacy` logger (C1) with the same identity fields as its zerolog logger. The dependency is `optional:"true"`, and a missing logger falls back to a `WireLegacy` logger built from `log.ConfigFromEnv`, never to `slog.Default()`. A half-migrated service therefore writes zerolog and slog lines with the same wire shape, and needs no zerolog-to-slog bridge.

The fx modules do not call `slog.SetDefault`. That call also sends the standard library `log` package through the slog handler (JSON instead of plain stderr), and a v0.22.0 consumer would see that change on upgrade without asking for it. A service gets the slog default when it moves its logging to `core/logging`. Code that logs outside the fx graph takes the logger explicitly: `errorenvelope.Options.Logger` (C3).

**Rejected alternatives.**

- **Big-bang switch to `nano gen wire`.** It is simpler to build, but a service would move 11 modules and ~149 providers (EP §2) in one PR. The task requires migration per module.
- **Keep two implementations** (fx module and nano module side by side). Every fix would be made twice, and behaviour would drift.

---

## C. Compatibility: wire contract and alerts

### C1. Log wire format

**Decision.** `core/log.Config` gets `Wire Wire`, with `WireSlog` (the zero value, today's output) and `WireLegacy`. `log.Settings` and `ConfigFromEnv` read it from `LOG_WIRE` (`slog` | `legacy`). `WireLegacy` installs a `ReplaceAttr` on the JSON handler (`core/log/config.go:162`):

| Field | zerolog today | `core/log` today | `WireLegacy` | Consumer |
|---|---|---|---|---|
| level | `level`: `debug`/`info`/`warn`/`error` | `"INFO"` etc. (`config.go:162`; asserted in `handler_test.go:93`) | lowercase. A custom level maps by range (below Info is `debug`, ... , Error and above is `error`) | `severity_text` (`otelcol.yaml:229`), alerts `in(error,fatal)` (`alert-rules.yaml:319`) and `in(warn,error,fatal)` (`:403`), dashboard `level:~` (`dashboards/logs/logs.json:1508`) |
| message | `message` | `msg` | `message` | `body` (`otelcol.yaml:226-227` accepts both) |
| source | `caller`: `/abs/path/file.go:64` | `source` group `{function,file,line}` | `caller`: `file:line` | split into `code.file.path`, `code.line.number` (`otelcol.yaml:239-243`) |
| duration | `Dur`: float milliseconds (`requestlog.go:151`) | `slog.Duration`: nanoseconds (`http.go:163`) | every `KindDuration` attribute as float milliseconds, as zerolog's global unit | `duration:>1000` (`alert-rules.yaml:362`), dashboard `p95_ms` (`dashboards/logs/logs.json`) |
| time | RFC 3339 | RFC 3339 nano | unchanged | not parsed from the app line (EP §4) |

Reasons: every Go service shares one collector and one set of alerts (EP §4, AN §4). With `WireLegacy`, a service migrates its logging without a lockstep infra PR, which AGENTS.md would otherwise require. The service sets `wire: legacy` in its committed YAML layer (`core/config` YAML layers, `core/config/doc.go`), not in the infra environment, so the logging PR needs no infra change. The option is a bridge: when `otelcol.yaml` normalizes the level case and maps `source.*` and when the alerts read the slog shape, services switch to `WireSlog` and the option is deleted.

Rejected: **lowercase by default.** The slog standard shape stays the framework default, because the pipeline is the thing to fix in the end state. Also rejected: **one option per field** (level case, key names, units). Four switches give 16 combinations to test, and only two of them are real.

### C2. Request summary parity

`pkg/httputil/requestlog` (`requestlog.go:129-163`) against `core/log.HTTPMiddleware` (`core/log/http.go:129-229`):

| Field | requestlog | core/log | Decision |
|---|---|---|---|
| `request_id`, `method`, `path` | `Contextualize` (`context.go:53-55`) | scope attrs (`http.go:136-141`) | Same keys. T2 exports `KeyMethod`, `KeyPath` and the other summary keys as constants, and a test pins them, because otelcol renames exactly these (`otelcol.yaml:251-252`). |
| `status`, `bytes_out`, `user_agent` | yes | yes | Same. |
| `client_ip` | yes (`requestlog.go:152`), first `X-Forwarded-For` hop (`requestlog.go:176-187`) | yes (`http.go:164`), rightmost hop that is not a trusted proxy | **Not new.** CR B5 lists it as new PII; that is wrong. The value differs when a proxy outside `TrustedProxies` or the client sets `X-Forwarded-For`. The second case occurs in prod: Traefik trusts 10.0.0.0/8 and keeps the incoming header (infra `stacks/shared.prod.yml:93-101`), so for `X-Forwarded-For: 1.2.3.4, 203.0.113.9` from the client, requestlog logs the spoofed 1.2.3.4 and core/log logs the real visitor 203.0.113.9. No alert reads `client_ip`. |
| `duration` | ms | ns | C1. |
| `query` | raw string (`requestlog.go:154`) | group, redacted (`http.go:193-212`) | With `LegacySummary`: one string, re-encoded from the redacted values. The raw query is never logged. |
| level of a cancelled request | Info (the handler's 499 line is Warn) | Warn (`http.go:220`) | With `LegacySummary`: Info. Otherwise each client cancel counts twice in the warn alert (CR A8). |
| message | `GET /p -> 200 (2.5ms)` (`requestlog.go:169-171`) | `http request` | Keep `http request`. No alert or dashboard reads the message text: the alerts filter on `status`, `severity_text`, `duration` and `http.request.method` (`alert-rules.yaml:278,319,362,403`). |
| `request_body` | optional, `LOG_REQUEST_BODY=true` (`requestlog.go:101-104`) | never (`http.go` doc, "The request body is never logged") | **Dropped.** Anchor's auth endpoints carry passwords, and key redaction cannot see inside a body. Anchor removes `LOG_REQUEST_BODY` from its environment in its migration PR (CR B8 lists the switch). |
| `route`, `client_canceled`, `hijacked`, `panic` | no | yes | Additive. otelcol stores them as attributes (`otelcol.yaml:225`). |

**Decision.** `log.HTTPOptions.LegacySummary bool` selects the two policy rows above. It is separate from `Config.Wire`, because one is the handler's encoding and the other is the middleware's policy.

### C3. HTTP error-envelope seam

**The seam already exists.** Three hooks receive every error the generated server produces:

1. `StrictHTTPServerOptions.RequestErrorHandlerFunc` and `ResponseErrorHandlerFunc` (`http/nanohttp/oapitemplates/strict/strict-http.tmpl:18-36`). They default to `nanohttp.RequestError` and `nanohttp.ResponseError` (`http/nanohttp/errors.go:14-25`). Body decode, generated validation (`strict-http.tmpl:63`) and handler errors go through them.
2. `StdHTTPServerOptions.ErrorHandlerFunc`, for parameter binding (`http/nanohttp/internal/example/api.gen.go:415-419`).
3. The router's 404 and 405 (C4).

oapi-codegen's default chi strict server, which both services use today, has the same two strict hooks (EP §5, `server.go:226-242`; AN §5.4). So one renderer serves the old and the new server.

**Decision: a slog renderer in `pkg/httputil/errorenvelope` (T2).** It must live in `pkg/`, because it renders `pkg/fault.Error` (the type the services' specs pin with `x-go-type`, EP §5, AN §5.2) and `pkg/search` errors, and a layered package may not import `pkg/`. It is a bridge, deprecated from the start, and it goes away with `pkg/fault`.

| Input error | Status | Body (`Content-Type: application/json`, trailing newline as `fault.WriteJSON`) |
|---|---|---|
| `*pkg/fault.Error` (or `Options.Adapter` result) | its status | its `errors` list, byte-identical to `fault.WriteJSON` |
| `search.ErrInvalidCursor`, `ErrCursorWithOffset`, `ErrUnknownSort` | 400 | `INVALID_CURSOR` field `pagination.cursor`, or `INVALID_SORT` field `sort_by`, with today's messages (`modules/httpserver/strict_error_handler.go:158-178`) |
| `core/fault.Described` from a generated catalogue | its status | one detail: `code` = `ProblemCode()` in **upper case** (`invalid_cursor` to `INVALID_CURSOR`), `message` = `ProblemDetail()`, or the title when there is no detail. `nano gen errors` requires lower snake codes (`gen/errors/parse.go:25,270`), and the mapping is one-to-one, so existing UPPER_SNAKE codes stay byte-identical (EP §5 "Error codes") without a generator change. |
| boundary errors from `http/problem` (malformed JSON, missing body, invalid parameters, validation) | 400 (a 422 from the boundary becomes 400) | one detail per field error: `code` = `Options.BoundaryCode` (default `BAD_REQUEST`), `message` = `<pointer or parameter>: <detail>`, `metadata` = `{field, rule}`. When the location is empty (the body root, for example a malformed JSON body or a body that is not an object), `message` is the detail alone and `metadata` has no `field`: `{"code":"BAD_REQUEST","message":"is not valid JSON at byte offset 1","metadata":{"rule":"syntax"}}`. The input value is never echoed (unlike `pkg/validate`, `error_mapper.go:28-46`). |
| body too large, unsupported media type | 413, 415 | `PAYLOAD_TOO_LARGE`, `UNSUPPORTED_MEDIA_TYPE`. These statuses are new (C6). |
| request cancelled, no status of its own | **499, no body, no Content-Type** | as `strict_error_handler.go` today (EP §5) |
| anything else | 500 | `UNEXPECTED_ERROR` / `Unexpected error` |
| any error on a route where `Options.IsStream(r)` is true | its status | `text/event-stream` frame `event: error` (EP §5, SSE row) |

API (T2), with `pkg/fault` types as the body:

```go
package errorenvelope // github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope

type Options struct {
	Logger       *slog.Logger                   // nil: slog.Default(), as http/problem.Write
	BoundaryCode string                         // default "BAD_REQUEST"
	Adapter      func(error) (*fault.Error, bool) // as modules/httpserver.APIErrorAdapter
	IsStream     func(*http.Request) bool
}

// Response is the resolution of one error: what to write and at which level to log.
type Response struct {
	Status     int
	Body       *fault.Error // nil: no body (499)
	Stream     bool
	Level      slog.Level
	Unmodelled bool
}

func Resolve(r *http.Request, err error, opts Options) Response

type Writer struct{ /* opts */ }

func New(opts Options) *Writer
func (w *Writer) RequestError(rw http.ResponseWriter, r *http.Request, err error)
func (w *Writer) ResponseError(rw http.ResponseWriter, r *http.Request, err error)

// PlainRequestError answers 400 text/plain with the first line of the
// parameter-binding error that err wraps (errors.As on the generated
// RequiredParamError, RequiredHeaderError, InvalidParamFormatError and the
// other binding types), or of err when it wraps none, as http.Error does:
// anchor's parameter-binding and validator errors today (AN §5.4).
func PlainRequestError(rw http.ResponseWriter, r *http.Request, err error)
```

`Writer` logs one `request failed` line through `core/log` with `status`, `error` and `route`, the keys `http/problem.Write` uses (`http/problem/write.go:39-61`). So the 5xx alert, which counts `status` (`alert-rules.yaml:278`), counts the same lines as today. That is two lines per 5xx, the same as v0.22.0 (CR A8).

`modules/httpserver.StrictErrorHandler` calls `Resolve` and keeps its zerolog logging. Then the 499, cursor-fault, SSE and adapter logic exists once, and a service still on zerolog sees no change.

**kin-openapi text: decision.** The framework does not reproduce library text. A service that keeps the kin-openapi middleware changes nothing. A service that adopts generated validation first updates every gate assertion that reads `$.errors[0].message` from a kin-openapi error. It follows the AGENTS.md procedure: export the flows, change each assertion to the status, `$.errors[0].code` and `$.errors[0].metadata.rule` or `metadata.field`, `flows publish` when a published version exists, and merge before the service PR. Anchor asserts message text twice (`"password"`, CR B1), and that comes from its own fault, not from kin-openapi.

Echopoint has 24 such assertions in 6 flows (exported from the prod CI org, workspace `ws_3LXAA3aNWjkEIGZRckhyYUk4zSC`). The generated detail texts are `must be at most N`, `must be at least N` and `must be at least 1 character long` (`gen/validate` golden `validate.gen.go`), so 7 assertions fail with the message format above:

| Flow | Request | Assertion on `$.errors[0].message` | C3 message |
|---|---|---|---|
| `d6a0a228-d53d-47ce-a3ac-3df2f15bd7b5` (method discipline) | `GET /definitely-not-a-real-route` | contains `no matching operation` | router 404 (C4) |
| `ce39b646-d18b-40ee-8b8b-2862bf67f644` (health monitors) | `GET /probes?limit=50&offset=0` | equals `no matching operation was found` | router 404 (C4) |
| `d6a0a228-d53d-47ce-a3ac-3df2f15bd7b5` | `GET /flows?limit=99999` | contains `maximum` | `limit: must be at most 100` |
| `d6a0a228-d53d-47ce-a3ac-3df2f15bd7b5` | `GET /flows?limit=-1` | contains `minimum` | `limit: must be at least 1` |
| `d6a0a228-d53d-47ce-a3ac-3df2f15bd7b5` | `GET /flows?limit=0` | contains `minimum` | `limit: must be at least 1` |
| `029bf60e-5bf9-4659-8361-a8fbb404c538` (webhooks) | `POST /webhooks {"name":""}` | contains `minLength` | `/name: must be at least 1 character long` |
| `029bf60e-5bf9-4659-8361-a8fbb404c538` | `GET /webhooks/{id}/analytics/requests/volumes` | contains `parameter "from" in query` | `from: is required` |

17 assertions pass with the message format by chance, because the location or the detail contains the asserted word. The procedure reviews them too, because a change of the location or the detail text breaks them:

| Flow | Assertions (step: asserted text) |
|---|---|
| `27016e93-1836-4c32-9183-46d9010c9eea` (cursor searches) | `my-unknown-sort`, `members-unknown-sort`, `invitations-unknown-sort`, `keys-unknown-sort`: `/sort_by` |
| `029bf60e-5bf9-4659-8361-a8fbb404c538` (webhooks) | `err_no_name`: `name`; `err_no_org`: `X-Organization-ID` |
| `4a9ac89e-d196-4427-856a-e7beb7abe5a9` (collections) | `err_no_name`, `err_folder_no_name`: `name`; `err_req_no_method`, `err_req_bad_method`: `method`; `err_list_no_pag`: `limit` |
| `f1b1a271-46e1-4fd2-ac84-918ec713f242` (catalog and org) | `err_members_no_pag`: `limit`; `err_resources_no_domains`, `err_resources_bad_domain`: `domains` |
| `ce39b646-d18b-40ee-8b8b-2862bf67f644` (health monitors) | `estimate-59s`: `at least 60`; `create-too-fast`: `interval_seconds` and `at least 60` (the spec bounds `interval_seconds` to 60..86400, so kin-openapi rejects these requests before the handler) |

The other `$.errors[0].message` assertions of echopoint read the service's own fault messages (for example `cron should not run more often than every 15 minutes`), which the renderer copies byte for byte (the `*pkg/fault.Error` row).

The message does not carry the rule code (for example `limit: must be at most 100 (maximum)`), although that would keep three of the failing assertions green. The rule is already in `metadata.rule`, and a gate that asserts a word inside a message stays coupled to text. The assertions move to the structured field instead.

Rejected: **reproduce kin-openapi messages** from `validate.Errors`. The wording differs per JSON Schema keyword and per kin-openapi release. The framework would then own the text of a library it no longer runs, and a kin-openapi bump already breaks it (CR B1). Also rejected: **a renderer option inside `http/problem`.** `http/problem` cannot import `pkg/fault` or `pkg/search` (layer rule), and RFC 9457 is its only contract (`CONTEXT.md`, "Problem").

### C4. Router: HEAD and 405 parity with chi

Facts. Go 1.22+ ServeMux answers a method mismatch with 405, an `Allow` header and a `text/plain` body. A `GET` pattern also matches HEAD. chi answers HEAD on a GET-only route with 405. The echopoint gate asserts 405 for POST, HEAD, PATCH, OPTIONS and DELETE on `/health` (CR A1, B1), so on ServeMux only the HEAD case breaks. Echopoint answers 400 `BAD_REQUEST` for an unknown path or method on spec routes, because its validator runs first (EP §5). Anchor answers 404 and 405 `text/plain` from its validator (AN §5.4).

**Decision (T2).** Add this to `http/nanohttp`:

```go
type RouterOptions struct {
	// NotFound answers a request that matches no pattern. Nil: a 404 problem.
	NotFound http.Handler
	// MethodNotAllowed answers a request whose path matches a pattern only for
	// other methods. The Allow header is already set. Nil: a 405 problem.
	MethodNotAllowed http.Handler
	// ExplicitHEAD makes a GET pattern stop matching HEAD: a HEAD request on a
	// path without a HEAD pattern goes to MethodNotAllowed, as chi does.
	ExplicitHEAD bool
}

func NewRouter(mux *http.ServeMux, opts RouterOptions) http.Handler
```

On a match, `NewRouter` calls the mux. On a miss, it asks `mux.Handler` for the same path with each standard method to build `Allow`, then calls `MethodNotAllowed` or `NotFound`. With `ExplicitHEAD`, a HEAD request whose matched pattern starts with `GET ` is a mismatch. The cost is paid only on a miss and on HEAD. The design relies on one property of `ServeMux.Handler`: it returns an empty pattern for a method mismatch and for no match, and a non-empty pattern (with a redirect handler) for a path it would redirect. T2's router tests check this property for each case, so a Go release that changes it fails the tests. Each service keeps its own statuses through its handlers: echopoint passes a `NotFound` and a `MethodNotAllowed` that write its 400 envelope, except for 405 on `/health`. Anchor passes `http.Error` handlers.

ServeMux also redirects unclean paths (`//a`, `/a/../b`) with 301, where chi answers 404 for no route. This difference is accepted and listed in each service PR. The golden HTTP suite (overnight decision 10) covers it.

Two more differences are accepted, found by a probe of chi v5.3.1 against `NewRouter` with `ExplicitHEAD` (8 methods, 13 paths). Each service PR lists them, and the golden HTTP suite covers them. `docs/http-boundary.md` has the list.

- **`Allow` on a 405 lists every method of the path.** For `GET` and `PATCH /webhooks/{id}`, `NewRouter` answers `Allow: GET, PATCH`, as RFC 9110 requires. chi lists only one method. The status is the same, and no exported gate flow asserts `Allow`.
- **A percent-encoded unreserved character matches the decoded route.** ServeMux matches on the decoded path, so `GET /he%61lth` is 200 and the other methods are 405 `Allow: GET`. chi matches on the raw path and answers 404.

Rejected: **generate `HEAD <path>` and method-less fallback patterns in `gen/oapi`.** This is deterministic, but it needs a second oapi-codegen template override (std-http, not the strict template), pinned to a version. It also misses the hand-registered routes, and `/health`, the route the gate asserts, is hand-registered outside the validator (`echopoint cmd/http/server.go:183`, CR A1). Also rejected: **keep chi in the framework.** ADR 0019 chose ServeMux, and chi middleware already has the `func(http.Handler) http.Handler` shape that works on any router (plan §23).

### C5. Start and stop timeouts

**Decision (T2).** Give `nano.AppConfig` config tags:

```go
type AppConfig struct {
	StartTimeout  time.Duration `config:"start_timeout"  default:"15s" json:"start_timeout"  yaml:"start_timeout"`
	StopTimeout   time.Duration `config:"stop_timeout"   default:"15s" json:"stop_timeout"   yaml:"stop_timeout"`
	HealthTimeout time.Duration `config:"health_timeout" default:"5s"  json:"health_timeout" yaml:"health_timeout"`
}
```

A service embeds it as `App nano.AppConfig `config:"app"``, so the env names are `APP_START_TIMEOUT`, `APP_STOP_TIMEOUT` and `APP_HEALTH_TIMEOUT`. `withDefaults` (`core/nano/app.go:35-46`) stays for code that builds an `AppConfig` by hand. A `gen/config` test binds a service `Config` that nests `nano.AppConfig`. That nesting of a struct from another package is not verified yet. T2 owns `gen/config` (E1), so if the loader (`gen/config/load.go:179,186-210,254`) needs a change for it, T2 makes it.

The default stop timeout stays 15s, the same as fx today (CR B3). Docker's default `stop_grace_period` is 10s, and neither prod stack sets it (`anchor.prod.yml`, `echopoint.prod.yml`; CR B3). The stop bound is `StopTimeout` plus `StopGrace` (1s, `core/nano/stop.go:19`) for each Stopper called after the timeout (decision log, "Stop gives each Stopper at least StopGrace"). So each service migration PR must also set `stop_grace_period` in its stack to at least `StopTimeout + StopGrace × Stoppers + 5s` (30s for both services). RELEASING.md lists this in the upgrade notes. Rejected: **lower the default to fit 10s.** Echopoint SSE streams hold `Shutdown` open (CR B3), and a lower default would cut drains short on every runtime with a longer grace, such as Kubernetes (30s).

### C6. Decoding: accepted differences

`nanohttp.Decode` uses strict json/v2. It rejects duplicate names, invalid UTF-8 and trailing data, matches names by case, and answers 413 and 415 (`docs/http-boundary.md:17`; EP §5 "Request decoding strictness", AN §5.7). The beta adds **no lenient mode**. Every released client is generated from the spec and sends exact member names and a JSON Content-Type: echopoint-cli (`internal/commands/errors.go:16-35`), the anchor Go SDK (`anchorsdk/errors.go`), and terraform-provider-anchor through that SDK (EP §5, AN §5.6). A lenient mode would reopen the ambiguities that ADR 0017 closed. A service that adopts `nanohttp` decoding lists 413, 415 and the strictness rows in its PR, and its golden suite checks them. For large uploads, it sets `nanohttp.MaxBodyBytes` (`http/nanohttp/decode.go:129`) per route.

---

## D. Release plumbing for nested modules

### D1. Tags and versions

| Module | Path | Tag | First beta |
|---|---|---|---|
| functional | `github.com/nanostack-dev/nanostack-framework/pkg/functional` | `pkg/functional/vX.Y.Z` | `pkg/functional/v0.23.0-beta.1` |
| db | `github.com/nanostack-dev/nanostack-framework/db` | `db/vX.Y.Z` | `db/v0.23.0-beta.1` |
| root | `github.com/nanostack-dev/nanostack-framework` | `vX.Y.Z` | `v0.23.0-beta.1` |

- **Independent versions** (ADR 0014, "modules version independently"). All three start at the same number so that the first beta is easy to recognize. After that, each one moves on its own, and the root `go.mod` records which nested versions it was tested with. Rejected: **lockstep sets.** They re-tag unchanged modules. A staged release (D3) also cannot promise one number when a nested module needs a fix in the middle of a beta.
- **`v0.23.0-beta.1`.** v0 allows a minor release to break the API (`docs/decision-log.md`, 2026-11-07 "Check API Compatibility With Consumer Builds"). This release breaks it: go-jet types change in the transactor (A1), and `pkg/functional` leaves the root module. The pre-release suffix keeps `go get ...@latest` and `go get -u` on v0.22.0, so no consumer moves by accident. The nano module APIs can still change after the first two migrations, in `beta.2` and later. Rejected: **`v0.23.0`** (it commits to APIs that no service has used yet) and **`v1.0.0-beta.1`** (it implies a compatibility promise the framework does not make).
- `db/tests` and `db/examples` are never tagged. They keep their local `replace` lines (`db/UPSTREAM.md`, "Nested modules").

### D2. go.mod rules

> **Amended on 2026-11-08** (decision log, "Development Form and Release Branches"). D2 and D3 below are the first text. The rules now in force are in [RELEASING.md](../../RELEASING.md#gomod-rules): `main` keeps the development form (`v0.0.0` with a `replace`), and the release commits live on a `release/vX.Y.Z` branch. The release form at a tag is unchanged.

1. A released module (`.`, `db`, `pkg/functional`) has no `replace` and no intra-repository pseudo-version in its `go.mod`. `go.work` stays git-ignored and dev-only (`.gitignore`, "Local Go workspace").
2. The root requires `pkg/functional` and `db`. `db` requires `pkg/functional`. The root no longer requires `github.com/go-jet/jet/v2`.
3. Consumers of the root get the `db` module's generator dependencies (`lib/pq`, `go-sql-driver/mysql`, `mattn/go-sqlite3`; `db/go.mod:5-12`) in their module graph and `go.sum`. Module graph pruning keeps them unbuilt unless a package imports them.
4. `lint/cmd/lintconfig` stays at its root path, because echopoint pins it as a `tool` (`echopoint go.mod:164`, CR B6).
5. Between releases, main may require an intra-repository **pseudo-version** of a pushed main commit when a dependent needs unreleased nested changes. The release check rejects it at tag time.

### D3. Release sequence (leaves first)

Each step is one commit on main, and each one builds with `GOWORK=off` against versions that already exist:

1. **functional.** Tag `pkg/functional/v0.23.0-beta.1` at the main commit that holds its final content. Wait until `GOPROXY=direct go list -m github.com/nanostack-dev/nanostack-framework/pkg/functional@v0.23.0-beta.1` succeeds.
2. **db.** One commit: drop the `replace` in `db/go.mod` (`db/go.mod:25-26`), require `pkg/functional v0.23.0-beta.1`, `go mod tidy`. CI passes. Tag `db/v0.23.0-beta.1`. Wait as above.
3. **root.** One commit (the PR that holds T1, T2 and T3, or its last commit): drop the `replace` (`go.mod:114-117`), require `pkg/functional v0.23.0-beta.1` and `db v0.23.0-beta.1`, `go mod tidy`. CI passes. Tag `v0.23.0-beta.1`.
4. **Verify.** The release workflow builds a scratch consumer against the three tags (D4).

**Dependencies of steps 1 and 2.** These steps depend on the content of `pkg/functional` and `db`, which is already in `f1bbfad` and `c46d30d`. They do not depend on T1, T2 or T3. They need PR #64 and the db-fork PR on main first. On 2026-11-08 no remote branch contains those commits (`git branch -r --contains c46d30d` prints nothing). So the orchestrator merges or pushes them before step 1.

**T1's branch.** T1 does not wait for the tags. It writes the root `go.mod` in its final form: no `replace`, and `pkg/functional v0.23.0-beta.1` and `db v0.23.0-beta.1` required. It builds and lints locally through a git-ignored `go.work` (`go work init . ./db ./db/tests ./pkg/functional`). `go.sum` cannot hold the hashes of the nested versions before they are tagged. So root CI on T1's branch (with `GOWORK=off`) stays red until step 2 is done, and step 3 then only runs `go mod tidy` and adds the commit and the tag. Until step 3, main keeps its `replace` for `pkg/functional` (`go.mod:117`).

Rejected for the bootstrap: **pin `db` at a pseudo-version of a pushed commit.** `db/go.mod` at that commit requires `pkg/functional v0.0.0` (`db/go.mod:11`), which does not exist. The root would then need its `replace` for `pkg/functional` anyway, so that pin only adds a `go.mod` state that lives for a few days.

Rejected: **all three tags at one commit.** `go.sum` would need hashes of versions that are not published yet. The only way to get them is to compute the module zip hashes by hand, which duplicates what the go command and the proxy do. Also rejected: **`replace` during development, stripped at release** (OTel crosslink style). CI would then test local code, not the versions in `go.mod`, against plan §19 ("CI runs each module with GOWORK=off, so the go.mod versions are what gets tested").

### D4. CI

- `ci.yml` and `db.yml` keep `GOWORK: "off"` (`ci.yml:18`, `db.yml:29`). The db lint job keeps its workspace (`db.yml:72-101`).
- **New job on every PR:** `go run ./internal/cmd/release check`. It fails when a released `go.mod` has a `replace`, and it lists intra-repository pseudo-versions as a warning (an error with `-tag`).
- **New `release.yml`** (`workflow_dispatch` with input `version`, plus tag pushes `v*`, `db/v*`, `pkg/functional/v*`). It runs `internal/cmd/release check -tag <version>`, checks that the tag order is leaves first and that each required intra-repository version exists, then builds a scratch module with `GOPROXY=direct`, `GOFLAGS=-mod=mod` and `go get` of the three tags. That module imports `pkg/functional`, `db/postgres` and `pkg/db/transactor`, then runs `go build` and `go vet`.
- `internal/cmd/release` is a small Go program in the root module with unit tests. Plan §20's `nano release` wraps it later, when `cmd/nano` gets a release command.

### D5. RELEASING.md and CHANGELOG.md

- `RELEASING.md` at the repository root is the runbook: the D2 rules, the D3 steps with exact commands, the verification, the rollback (a tag is never moved or deleted; publish the next pre-release), and the consumer upgrade notes (D6, C5 `stop_grace_period`). `docs/runbooks/deployment.md` links to it for tagging and keeps the consumer-build compatibility check (`deployment.md:5-6`).
- `CHANGELOG.md` uses one section per module and version, newest first, with Added, Changed, Deprecated, Removed and Fixed. The first entries are in the appendix. Only T1 edits it. T2 and T3 hand over their entries in their reports.

### D6. Consumer upgrade from v0.22.0

1. `go get github.com/nanostack-dev/nanostack-framework@v0.23.0-beta.1`, then `go mod tidy`. Tidy adds `pkg/functional v0.23.0-beta.1` and `db v0.23.0-beta.1`. The `pkg/functional` import path does not change (CR A12).
2. Upgrade the root and `pkg/functional` together. A graph that keeps root v0.22.0 (which contains `pkg/functional`) next to the `pkg/functional` module fails with "ambiguous import".
3. Do the A6 steps (go-jet imports, regeneration, Raw and Option checks).
4. Nothing else is required. fx modules, zerolog and the legacy HTTP stack keep working (B5), and the fx modules install no slog default, so standard library `log` output does not change. Each later step (logging with `WireLegacy`, typed config, module moves, envelope writer) is a separate service PR with the golden HTTP suite before and after (overnight decision 10).
5. Anchor (v0.20.0) first reads the v0.21 and v0.22 notes. Its line references were checked against v0.20.0 (CR A13).

---

## E. Work split

### E1. File ownership

Each path has exactly one owner. A task that needs a change in a path it does not own records the request in its report and does not make the change.

| Path | T1 (A + D) | T2 (C) | T3 (B) |
|---|---|---|---|
| `pkg/db/**`, `pkg/jetx/**`, `pkg/testkit/searchtest/**` | ✔ | | |
| `go.mod`, `go.sum` (root), `db/go.mod`, `db/go.sum`, `db/tests/go.{mod,sum}`, `db/examples/go.{mod,sum}`, `pkg/functional/go.mod` | ✔ | | |
| `db/UPSTREAM.md` ("Release order", "Dependency" row), `db/README.md` | ✔ | | |
| `.github/workflows/**` (`ci.yml`, `db.yml`, new `release.yml`) | ✔ | | |
| `internal/cmd/release/**` (new) | ✔ | | |
| `RELEASING.md`, `CHANGELOG.md` (new), `docs/runbooks/**`, `docs/decision-log.md`, `docs/README.md`, `.gitignore` | ✔ | | |
| `core/log/**`, `core/nano/**` | | ✔ | |
| `http/problem/**`, `http/nanohttp/**` | | ✔ | |
| `gen/config/**` (the nested `nano.AppConfig` test, and a loader fix if that test needs one) | | ✔ | |
| `pkg/httputil/errorenvelope/**` (new), `pkg/httputil/requestlog/**` (deprecation notice) | | ✔ | |
| `modules/httpserver/strict_error_handler.go`, `strict_error_handler_test.go` | | ✔ | |
| `docs/http-boundary.md` | | ✔ | |
| `pg/**`, `redis/**`, `otel/**`, `core/logging/**` (new) | | | ✔ |
| `http/nanoserver/**`, `http/nanoapisec/**`, `http/nanopprof/**` (new) | | | ✔ |
| `modules/**` except the two `strict_error_handler` files; `modules/nanofx/**` (new); `app/**` (deprecation notice) | | | ✔ |
| `internal/layers/**`, `.golangci.local.yml`, `.golangci.yml` (generated by `lintconfig`) | | | ✔ |
| `AGENTS.md`, `CONTEXT.md`, `docs/generation.md`, `docs/technical/architecture.md`, `docs/boundaries.md`, `starter/README.md` | | | ✔ |
| `internal/nanoexample/**` (new: a manifest that wires every nano module; `nano gen wire -check` and `nano gen config -check` run on it) | | | ✔ |

`gen/oapi` needs no change: the hooks of C3 already exist. T3 adds no new `go.mod` requirement. Everything it imports (`cirello.io/pglock`, `XSAM/otelsql`, `redis/go-redis/v9`, `golang-migrate/migrate/v4`, OpenTelemetry) is already required (the `go.mod` require block, `go.mod:7-40`). If `go mod tidy` still changes `go.sum`, T3 reports the diff to T1.

### E2. Contracts

**T1 to T3.** Same names and shapes. Only the jet and qrm import paths change:

```go
package transactor // github.com/nanostack-dev/nanostack-framework/pkg/db/transactor

import (
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
)

func New(db *sql.DB) Transactor
func Executor(ctx context.Context, db qrm.DB) qrm.DB
func Query[T any](ctx context.Context, db qrm.DB, stmt jet.Statement) Result[T]
func Exec(ctx context.Context, db qrm.DB, stmt jet.Statement) Result[struct{}]
// ... every other exported identifier of transactor.go, row_lock.go, page.go and keyset.go as today
```

T1 also guarantees that `pkg/db/transactor`, `pkg/db/pgerr`, `pkg/jetx` and `pkg/search` keep zero fx and zerolog dependencies (the B1 allow-list check).

**T2 to T3.**

```go
package log // github.com/nanostack-dev/nanostack-framework/core/log

type Wire int

const (
	WireSlog Wire = iota // default: slog's JSON shape
	WireLegacy           // lowercase level, message, caller file:line, durations in ms
)

type ContextAttrs func(context.Context) []slog.Attr

// Config gains: Wire Wire.

type Settings struct {
	Level       string `config:"level"       env:"LOG_LEVEL"       default:"info"`
	Format      string `config:"format,optional" env:"LOG_FORMAT"` // empty: auto, as ConfigFromEnv
	Wire        string `config:"wire"        env:"LOG_WIRE"        default:"slog"`
	Environment string `config:"environment" env:"ENVIRONMENT"     default:"development"`
	Service     string `config:"service,optional" env:"SERVICE_NAME"`
	Version     string `config:"version,optional" env:"SERVICE_VERSION"`
}

func (s Settings) Config() (Config, error)

const (
	KeyMethod   = "method"
	KeyPath     = "path"
	KeyDuration = "duration"
	// plus KeyBytesOut, KeyClientIP, KeyQuery, KeyUserAgent
)

// HTTPOptions gains: LegacySummary bool.
```

```go
package nano // AppConfig with config tags, as section C5.

package nanohttp
type RouterOptions struct{ NotFound, MethodNotAllowed http.Handler; ExplicitHEAD bool }
func NewRouter(mux *http.ServeMux, opts RouterOptions) http.Handler

package errorenvelope // as section C3
```

T3 may start against stubs of these signatures. The final merge order is T1 and T2, then T3.

### E3. Order

1. T1 and T2 run in parallel. Their files are disjoint, and T2 needs no `go.mod` change.
2. Release steps D3.1 and D3.2 (functional and db tags) run as soon as PR #64 and the db-fork PR are on main. They do not wait for any task (D3, "Dependencies of steps 1 and 2"). After them, root CI on the task branches can turn green.
3. T3 builds on T1 and T2. Root CI is green once the nested tags exist.
4. The orchestrator adds the T2 and T3 changelog and decision-log entries (pre-written in the appendix) to T1's files, then runs D3.3 and D3.4.

### E4. Acceptance per task

Every task: root lint 0 issues, `go run ./lint/cmd/lintconfig -check` ok, `go test ./...` passing, and every behaviour change has a test. T1 also runs db lint (`lintconfig -check -dir db`, lint in `db/` and `db/tests` within a `go.work`, as `db.yml`).

- **T1.** The transactor, jetx and searchtest unit and integration suites pass against the fork. The row-lock SQL test (A4.3) passes. `internal/cmd/release` tests cover: a replace in a released module, an intra-repository pseudo-version under `-tag`, a missing required tag, and a wrong order. `release.yml` dry-run evidence. The echopoint and anchor model diffs (A4.4) are in the report.
- **T2.** A golden test: one request through `requestlog` (zerolog) and through `core/log` with `WireLegacy` + `LegacySummary` gives the same keys, the same lowercase level and a ms `duration`. `route` and `client_canceled` are the only additive keys. Tests pin the key constants. `errorenvelope` ports every `strict_error_handler_test.go` case (`:32-399`) and gets identical status and body. Boundary-error, upper-case-code, 413/415, 499 and SSE rows have tests. Router tests: HEAD on GET with and without `ExplicitHEAD`, 405 with `Allow`, custom NotFound, a ServeMux redirect is unchanged. `gen/config` binds a nested `nano.AppConfig` from `APP_START_TIMEOUT`. A `core/nano` test: a configured `StartTimeout` bounds start.
- **T3.** Unit tests per provider. Integration tests for `nanopg`, `nanomigrate` (both ported tests), `nanolock` and `nanocache` (Redis through `pkg/testkit/redistest`). Tests: tracing flushes on Stop, `nanoserver` drains on cancel, the `nanofx.Lifecycle` bridge runs Start, Stop and Run in an fx app, each fx wrapper keeps its fx signature (an fx app built the v0.22.0 way still starts), the layers test ranks `pg`, `redis` and `otel` as layer 2 and enforces the allow-list, and `internal/nanoexample` passes `nano gen wire -check`.

---

## T2 implementation notes (deviations from this design)

T2 implemented section C in `core/log`, `core/nano`, `http/` and the generated templates. These points differ from the text above:

1. **The seam needed one template change.** The three hooks exist, but the generated `NewHTTPHandler` set them to `nanohttp.RequestError`, `nanohttp.ResponseError` and `problem.Write`, with no option to replace them. A service on `NewHTTPHandler` could not plug in an envelope writer, and parameter binding errors were only available as unexported oapi-codegen types. `HTTPHandlerOptions` now has `RequestErrorHandlerFunc` and `ResponseErrorHandlerFunc`. Parameter binding errors go to the request hook as `problem.InvalidParameter`, which names the parameter. The change is in `http/nanohttp/oapitemplates/strict/strict-http.tmpl`; `gen/oapi` has no change. The 15 generated `api.gen.go` files were regenerated.
2. **`NewRouter` looks up every request twice, not only a miss and HEAD.** `ServeMux.ServeHTTP` sets `r.Pattern` and calls the handler in one step, so the router cannot see a miss without a lookup of its own first. The extra lookup costs about 100 ns and one allocation (`BenchmarkRouterMatch`). A wrapper that catches the mux's own 404/405 writes would hide `http.Flusher` and `http.Hijacker`, and could not tell the mux's 404 from a handler's 404.
3. **ServeMux redirects with 307 on Go 1.27, not 301.** `NewRouter` sends every request with an unclean path to the mux, for every method, so it keeps the ServeMux redirect also when the clean path has no route (`//nothing`) or a route only for other methods (`POST //health`). (Review fix: the first version answered `//nothing` with `NotFound` and `POST //health` with 405 `Allow: GET`, a third answer that matched neither chi nor ServeMux.)
4. **The legacy summary line has no `caller`.** The zerolog summary line has `caller`, which always names `requestlog.go:162`. The slog summary is written with no source (pc 0). No alert reads the caller of the summary line. The golden test lists `route` as the only additive key and `caller` as the only missing key.
5. **`query` with `LegacySummary` keeps the raw pairs.** It is the raw query in its order and encoding, with each secret value replaced by `[REDACTED]`, not a re-encoding of `url.Values`. So it is byte-identical to the zerolog line when no parameter is secret and no pair is one that `url.ParseQuery` rejects (a malformed name, or a semicolon, which could hide a secret in the value of a public name).
6. **Out of T2's path scope.** The task gave T2 only `core/log`, `core/nano`, `http/`, `gen/oapi` and `gen/validate`. These C items were not done by T2: `pkg/httputil/errorenvelope` (C3), the `Resolve` call in `modules/httpserver.StrictErrorHandler`, the deprecation notice in `pkg/httputil/requestlog`, and the decision-log and `CONTEXT.md` entries. The second review fix built `errorenvelope` and the `Resolve` call (see "Second review fix notes"). `gen/config` needs no change: a scratch service that nests `nano.AppConfig` and `log.Settings` bound `APP_START_TIMEOUT=42s` and `LOG_WIRE=legacy` with today's loader.

## T3 implementation notes (deviations from this design)

T3 built the nano modules of section B: `core/logging`, `otel/nanotrace`, `pg/nanopg`, `pg/nanomigrate`, `pg/nanolock`, `redis/nanocache`, `http/nanoserver`, `http/nanopprof` and `modules/nanofx`. `docs/modules.md` is the guide. These points differ from the text above:

1. **No `nanoserver.HealthHandler`.** `nanoserver.Readiness` replaces it: it has the method set of `health.Checker`, and the service registers it in its own `pkg/health` registry, so one implementation of the legacy body stays. (T3 also left out `http/nanoapisec`, because the B1 allow-list was not in its paths. The allow-list now has `pkg/apisec` and `pkg/health`, and the second review fix added `http/nanoapisec`.)
2. **The B1 rank map and allow-list are not done.** `pg/`, `redis/` and `otel/` are unchecked today (`Layer` returns 0). Rank them as layer 2 and add the allow-list in the same change: `pg/nanopg` imports `pkg/db/transactor` and `otel/nanotrace` imports `pkg/tracing`, so the rank alone breaks the layers test.
3. **The fx modules are not thin wrappers (B3), and `modules/cache.Store` is not an alias.** T3 could change only doc comments and READMEs in `modules/`. Two implementations exist in the beta. `nanocache.Store` has the methods of `cache.Store`, so one value satisfies both, but the two `ErrCacheKeyNotFound` values differ.
4. **Signatures.** `logging.NewLogger` returns `(*slog.Logger, error)`, because an invalid `LOG_LEVEL` must fail the build. `nanotrace.NewProvider` also takes `log.Settings`: `gen/config` rejects a second field with the env `SERVICE_NAME` (`gen/config/load.go`, `leaf`). `nanotrace.Installer` installs the global provider in `Start`, not at construction: a library provider that nothing takes is not built, and a `Starter` is always built. The flush is the start-less `Stop` of `nanotrace.Provider`, so it runs even when the build fails after `NewProvider` (beta audit). `nanocache.NewStore` returns `Store` only, because it never fails (it falls back to the no-op store).
5. **Prose deprecation notices, not `// Deprecated:` paragraphs.** The formal marker gives a staticcheck SA1019 issue at each import of the package: four inside `modules/`, and many in echopoint and anchor, whose shared lint config enables every staticcheck check. That contradicts D6 ("Nothing else is required"). Use the formal marker in the release after the two service migrations.
6. **`/health` body.** It has `"readiness": "HEALTHY"` instead of the per-dependency keys of the fx modules, such as `"postgres"`, and it answers 503 while the App starts or stops. No gate assertion on these keys is known; this is not verified.
7. **`internal/nanoexample` is not built** (not in T3's paths). A scratch service with a manifest of all eight modules passed `nano gen wire -check`, and its generated `Bind` bound every module from env names.

## Second review fix notes (deviations from this design)

A second review found that C3 had no implementation and that the B1 allow-list made the reason for the missing `http/nanoapisec` false. These points were built, and differ from the text above:

1. **`pkg/httputil/errorenvelope` (C3) is built.** `Resolve(r, err, opts)` maps a handler error and `ResolveRequest(r, err, opts)` a request-hook error. `Response` has `Status`, `Body`, `Stream`, `Unmodelled` and `Abandoned`, and a `Write` method. It has no `Level`: `Writer` logs through `core/log.ErrorStatusTo` (Debug for a 4xx, Warn for 499, Error for a 5xx, an error's own `Level()` first), and `StrictErrorHandler` keeps its v0.22.0 zerolog levels and messages. Each picks its level from the `Response` fields.
2. **A request-hook error that is not a boundary problem keeps its text.** The C3 table has no row for it. `ResolveRequest` answers it 400 `BAD_REQUEST` with `err.Error()`, as `StrictErrorHandler` does in v0.22.0 for the errors of oapi-codegen's chi strict server (`can't decode JSON body: ...`). So a service on the chi strict server sees no change.
3. **Boundary problems without field errors** (missing body, unreadable body) give one detail with the problem's detail as the message and no metadata. A boundary 404 or 405 keeps its status, with code `NOT_FOUND` or `METHOD_NOT_ALLOWED`.
4. **Router writers.** `Writer.NotFound()` and `Writer.MethodNotAllowed()` write 404 `NOT_FOUND` and 405 `METHOD_NOT_ALLOWED`. `Writer.Handler(*fault.Error)` writes a service's own answer, such as echopoint's 400 `no matching operation was found`.
5. **`PlainRequestError`** reads the binding error as the cause of `problem.InvalidParameter` or `problem.MissingParameter` (generated binding types are per package, so a framework package cannot name them). A problem without a cause answers its first field error as `<location>: <detail>`.
6. **The unmodelled 500 line** of `Writer` also carries `error_type` and `error_detail`, as the zerolog line of `StrictErrorHandler` does, because it is the only record of the cause.
7. **`http/nanoapisec`** is built as B3 says, and `modules/apisec` is its thin wrapper. `internal/nanoexample` lists it.

## Beta audit fix notes (deviations from this design)

The audit of 2026-11-08 changed these points. The [decision log](../decision-log.md#2026-11-08-beta-audit-fixes) has the alternatives.

1. **Echopoint's `WithAuth` must refuse a request the resolver does not match** before echopoint adopts `nanohttp.NewRouter` or drops its kin-openapi request validator. Until then the validator refuses unknown paths; after, an unmatched request would pass without requirements. Echopoint's own tests that call `apisec.NewResolver(doc)` bare (`internal/security/contract_test.go`, `internal/middleware/module_test.go`) must pass `apisec.MatchChi()`. Without it they build the ServeMux matcher, which refuses echopoint's spec (point 2).
2. **Echopoint cannot move to `nanohttp.NewRouter` yet.** Its spec has paths that conflict in `http.ServeMux` (`DELETE /flows/{id}/variables` against `/flows/folders/{id}`), so `NewRouter` and the ServeMux matching of `apisec.NewResolver` refuse it. It keeps chi, and `modules/apisec` matches as chi (`apisec.MatchChi`). The paths must be reshaped first. Anchor's spec (111 operations) matches the same in both modes.
3. **The stop budget includes `DrainDelay`.** `nano.AppConfig.DrainDelay` keeps the Runners serving after readiness turns unavailable, inside `StopTimeout`. Start fails when it leaves the Runners less than `StopGrace` (1s) of `StopTimeout`. A service that sets it raises `StopTimeout` by the same amount, keeps `shutdown_timeout` below `StopTimeout − DrainDelay`, and keeps `stop_grace_period` at least `StopTimeout + StopGrace × Stoppers + 5s` with the raised `StopTimeout`.
4. **fx services provide a context.** The dialing constructors (`nanopg.NewDB`, `nanocache.NewStore`, `nanolock.NewClient`) take a `context.Context`, which an fx graph does not have: `nanofx.Context(signalCtx)` provides it. Tracing on fx registers `nanotrace.Installer` next to `nanotrace.Provider`.
5. **Module configs have one zero-value rule.** A constructor defaults a zero duration or size; `DefaultConfig()` gives the other defaults to a `Config` built by hand.

## Not verified, and open items

- **Not verified:** gate flows that assert `$.errors[0].metadata.field` (anchor, 2) compare it with a go-playground field name or with a JSON member name. T2's boundary mapping (C3) uses the location. The anchor migration PR checks this against the exported flows (`scratchpad/migration/flows/`).
- **Not verified:** a rollback to an old image after pgkit `CompleteSchema` (CR B3). This is pgkit work, outside this design.
- **Open:** echopoint links `echopoint-runner` packages that log through global zerolog (CR B5). This is a service item. The framework adds no zerolog bridge.
- **Open:** the `_FILE` `SecretProvider` for `core/config` (EP §3, AN improvements item 3). It is not needed for the beta (B4).
- **Open:** slog sampling for anchor's one zerolog `Sample` call (AN §2). It is not needed for the beta. Anchor removes the sampling or writes a small handler.

## Appendix: pre-written entries

### Decision log

The entries that this section pre-wrote were not added as written. `docs/decision-log.md` records what was decided and built, in three entries of 2026-11-08:

- **Beta Readiness Review Fixes.** Design B3 slips past the first beta: each nano module is its own implementation, and the fx wrappers come in beta.2. The integration layer is enforced with the closed `pkg` allow-list. A missing parameter keeps its binding error. `NewRouter` keeps every ServeMux redirect. `pg/nanomigrate` gives its connection back and reports a stop. The fx bridge gives a late Stop the stop grace. `internal/nanoexample` wires every nano module.
- **Beta Readiness Second Review Fixes.** The legacy error envelope (C3) ships in beta.1, with its deviations. `http/nanoapisec` exists and `modules/apisec` wraps it. `pkg/functional` is the leaf layer. Two more router differences from chi are accepted. `pg/nanomigrate` reads the stop only after its watcher ends.

The decisions on the database helpers (A), the log wire (C1, C2), the timeouts (C5) and the nested releases (D) are in this design and in `CHANGELOG.md`. The release PR adds their decision-log entries.

### CHANGELOG.md (first entries)

Superseded by `CHANGELOG.md`, which lists what beta.1 ships.

```markdown
# Changelog

## v0.23.0-beta.1 (root)

### Added
- `core/` (secret, log, config, fault, validate, nano), `http/` (problem, nanohttp), `gen/` and `cmd/nano`.
- Nano modules: `core/logging`, `otel/nanotrace`, `pg/nanopg`, `pg/nanomigrate`, `pg/nanolock`, `redis/nanocache`, `http/nanoserver`, `http/nanoapisec`, `http/nanopprof`; `modules/nanofx` bridge.
- `core/log`: `Config.Wire` (`WireLegacy`), `Settings`, `HTTPOptions.LegacySummary`, exported summary keys.
- `nanohttp.NewRouter`; `pkg/httputil/errorenvelope`; config tags on `nano.AppConfig`.

### Changed (breaking)
- `pkg/db/transactor` and `pkg/jetx` take Nanostack DB types (`github.com/nanostack-dev/nanostack-framework/db/...`) instead of go-jet. Rewrite go-jet imports and regenerate models.
- `pkg/functional` is its own module (same import path). `go mod tidy` adds it.

### Deprecated
- Every fx module in `modules/`, `app/`, `pkg/fxlog`, `pkg/httputil/requestlog`, `modules/httpserver.StrictErrorHandler`. They keep working in the beta.

## db/v0.23.0-beta.1
- First release of Nanostack DB: go-jet d0e152c (v2.16.1 + #628) fork with `functional.Option` support, `query`, `filter`, `stmtcache`, `-nullable=option`. See `db/UPSTREAM.md`.

## pkg/functional/v0.23.0-beta.1
- First release as its own module. Adds `Option.Scan` and JSON encoding.
```
