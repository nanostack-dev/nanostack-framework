# Implemented architecture

The repository is a Go module consumed by applications; it does not deploy a production service. [go.mod](../../go.mod) owns its toolchain and dependency identities. Product contracts and schemas remain in consuming repositories.

| Area | Implemented responsibility | Reference |
| --- | --- | --- |
| `app` | Fx service composition | [Assembly API](../../app/app.go) |
| `modules` | Config, logging, PostgreSQL, cache, migrations, locks, HTTP, security, profiling and transaction lifecycle wiring | [Module boundaries](../boundaries.md#module-rules) |
| `pkg` | Functional values, transaction/query helpers, SQL error classification, health, logs, validation, IDs, secrets, HTTP helpers and generic test infrastructure | [Package boundary](../../pkg/README.md) |
| `gen/oapi`, `gen/jet` | Generation configuration conventions | [Generation](../generation.md) |
| `gorules` | Ruleguard rules consumed by application lint configuration | [Functional rules](../../gorules/functional.go), [transaction rules](../../gorules/transactor.go) |

`cli/` and `starter/` currently contain design documents, rather than a complete runnable scaffolder. The [original architecture](../architecture.md) and [roadmap](../roadmap.md) describe planned scope and should not be read as shipped commands.

## Runtime boundaries

Applications combine framework modules with their own adapters through Fx. Infrastructure lifecycle hooks own start/stop behavior; products retain authorization, tenant resolution, queue payloads, retries and transport policies. Core packages should be usable without requiring an application's Fx graph.

Database execution follows the context-carried [transactor](../../pkg/db/transactor/README.md). The outer transaction owns commit/rollback, query results allow caller-specific constraint translation, and row-lock helpers require a transaction. Scan destinations must match go-jet column prefixes. `jetx` supplies expressions and filters rather than a competing query executor.

The [functional package](../../pkg/functional/README.md) separates absence, failure and accumulating validation. Consult current function signatures when adopting it: historical decision entries and older examples may describe a previous API shape.

## Compatibility

Consumers pin the same module/package identity across an Fx graph. Mixing old and new injected types or relying on a sibling `replace` directive can conceal an integration failure. Validate public changes in the affected consumer modules and their database/contract suites; the framework's pure tests alone cannot prove application compatibility.
