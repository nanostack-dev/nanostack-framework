# Implemented architecture

The repository is a Go module consumed by applications; it does not deploy a production service. [go.mod](../../go.mod) owns its toolchain and dependency identities. Product contracts and schemas remain in consuming repositories.

| Area | Implemented responsibility | Reference |
| --- | --- | --- |
| `core/nano` | Runtime of generated wiring: `App` (start in dependency order, Runners, stop in reverse under one deadline with a per-Stopper grace), lifecycle interfaces, `/livez` and `/readyz`, `Main`, manifest declarations | [Wiring guide](../generation.md#wiring-nano-gen-wire) |
| `app` | Fx service composition, until services move to `nano gen wire` | [Assembly API](../../app/app.go) |
| `modules` | Config, logging, tracing, PostgreSQL, cache, migrations, locks, HTTP, security, profiling and transaction lifecycle wiring | [Module boundaries](../boundaries.md#module-rules) |
| `pkg` | Functional values, transaction/query helpers, SQL error classification, health, logs, tracing, validation, IDs, secrets, HTTP helpers and generic test infrastructure | [Package boundary](../../pkg/README.md) |
| `core/validate`, `core/fault` | Runtime of generated request validators and error catalogues | [HTTP boundary](../http-boundary.md) |
| `http/nanohttp`, `http/problem` | Strict request decoding, generated validation and RFC 9457 problem responses | [HTTP boundary](../http-boundary.md) |
| `gen` | Generator contract: `gen.File`, `gen.Error`, `gen.ErrStale`, `gen.ErrEnvironment`, `gen.Sync`, `gen.Report`, `gen.Command` | [Generator contract](../../gen/README.md) |
| `gen/wire` | `nano gen wire`: manifest to `wire.gen.go` with `Build`, `BuildComponents`, typed `Overrides`, `-check` | [Wiring guide](../generation.md#wiring-nano-gen-wire) |
| `gen/config`, `gen/errors` | `nano gen config` (binder, `CONFIG.md`, `.env.example`) and `nano gen errors` (typed errors, `ERRORS.md`) | [Generation](../generation.md) |
| `gen/cmd/nano` | The one CLI: `nano gen config\|errors\|validate\|wire`, each with `-check`. Services run it as a `tool` | [Checking generated files](../generation.md#checking-generated-files) |
| `core/config`, `core/log`, `core/secret` | Configuration runtime, slog logging and redaction, and the one secret type both use | [core/log](../../core/log/README.md), [Generation](../generation.md) |
| `gen/oapi`, `gen/validate`, `gen/jet` | Generation configuration conventions; `nano gen validate` writes request validators from the OpenAPI document | [Generation](../generation.md) |
| `gorules` | Ruleguard rules consumed by application lint configuration | [Functional rules](../../gorules/functional.go), [transaction rules](../../gorules/transactor.go) |

`cli/` and `starter/` currently contain design documents, rather than a complete runnable scaffolder. The [original architecture](../architecture.md) (history) and [roadmap](../roadmap.md) describe planned scope and should not be read as shipped commands.

## Layers

New code lives in three layers. A package imports only its own layer and the layers below (plan section 06):

1. `core/`: the runtime that services and generated code call. It imports the standard library, small libraries and other `core/` packages.
2. The integrations: `http/` (the HTTP boundary) and the nano modules in `pg/`, `redis/` and `otel/`. They import `core/` and each other.
3. `gen/`: the generators and the CLI (`gen/cmd/nano`). They may import tooling such as `golang.org/x/tools`. `gen` is its own module: it requires the root module, and the root module never requires it, so no runtime package can import it and its dependencies stay out of a runtime consumer's module graph.

No layered package imports the legacy trees `modules/` or `app/`, Fx or zerolog. No layered package imports `pkg/` either, with one listed exception (design B1): an integration package may import the primitives in `internal/layers.AllowedLegacy`, which carry no Fx and no zerolog, until each one moves to its layered home. The [`internal/layers`](../../internal/layers/layers.go) test checks the import graph, test and integration files included, and that no runtime layer depends on Fx or zerolog. The depguard rules in [`.golangci.local.yml`](../../.golangci.local.yml) and [`gen/.golangci.local.yml`](../../gen/.golangci.local.yml) give the same rules in editors and in lint.

## Runtime boundaries

Applications combine framework modules with their own adapters through Fx today. Generated wiring (`core/nano` and `nano gen wire`) replaces it: modules move from `fx.Lifecycle` hooks to the `Starter`, `Stopper`, `Runner` and `HealthChecker` interfaces, then services move one Fx module at a time into a manifest. Infrastructure lifecycle hooks own start/stop behavior; products retain authorization, tenant resolution, queue payloads, retries and transport policies. Core packages should be usable without requiring an application's Fx graph.

Database execution follows the context-carried [transactor](../../pkg/db/transactor/README.md). The outer transaction owns commit/rollback, query results allow caller-specific constraint translation, and row-lock helpers require a transaction. Scan destinations must match go-jet column prefixes. `jetx` supplies expressions and filters rather than a competing query executor.

The [functional package](../../pkg/functional/README.md) separates absence, failure and accumulating validation. Consult current function signatures when adopting it: historical decision entries and older examples may describe a previous API shape.

## Observability

[Tracing](../../pkg/tracing/README.md) follows the standard `OTEL_*` environment and is a no-op without an OTLP endpoint. `modules/httpserver` starts a server span per request (continuing the edge's W3C `traceparent`) and names it after the chi route; `modules/postgres` records queries as child spans; `tracing.Client`/`tracing.Transport` propagate to downstream services. Log lines carry `trace_id`/`span_id` through `requestlog.Contextualize` and `log.TraceEnricher`, which `modules/logging` registers.

## Compatibility

Consumers pin the same module/package identity across an Fx graph. Mixing old and new injected types or relying on a sibling `replace` directive can conceal an integration failure. Validate public changes in the affected consumer modules and their database/contract suites; the framework's pure tests alone cannot prove application compatibility.
