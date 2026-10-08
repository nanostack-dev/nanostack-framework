# Nanostack Framework agent guide

This repository contains reusable Go service primitives and lifecycle wiring. Its local guides are sufficient for standalone work; a shared workspace or installed skills is an optional enhancement.

## Read by task

- Domain names or ownership: read [CONTEXT.md](CONTEXT.md) and [boundaries](docs/boundaries.md).
- Package changes or service integration: read [current architecture](docs/technical/architecture.md), then the affected package guide in [the index](docs/README.md).
- First checkout or environment failure: read [setup](docs/development/setup.md) and [troubleshooting](docs/development/troubleshooting.md).
- Code changes: follow [testing](docs/development/testing.md); database behavior needs the documented PostgreSQL integration tests.
- Public API changes or release work: read [publishing and consumer upgrades](docs/runbooks/deployment.md), [rollback](docs/runbooks/rollback.md) and [existing decisions](docs/adr/README.md).

## Boundaries and invariants

- Keep reusable primitives in `pkg/` and thin Fx lifecycle wiring in `modules/`. Products own tenancy, authorization policy, domain models, queue names and payloads, generated application code and fixtures.
- Reuse the existing framework primitive before adding a parallel API. Keep public errors, cancellation and compatibility explicit; do not silently discard failures.
- Use `pkg/functional` for collection transformations and optional/result values. Read its current source and [API guide](pkg/functional/README.md): absence and failure are separate states. Plain loops are appropriate for side effects, early exit or a functional form that reads worse; state the reason in review.
- Pass `context.Context` first where needed, respect cancellation and wrap propagated errors with `%w`.
- Database query execution uses `pkg/db/transactor`; `pkg/jetx` owns expression/filter helpers. Keep constraint names application-supplied and transport errors out of the data layer. Use generated go-jet model scan destinations, or alias every column for a custom destination; enable strict scanning in affected integration suites.
- Durable product work belongs in a database-backed queue or persisted state, with cancellation and joined shutdown for worker lifecycles. Framework code does not invent product scheduling semantics.
- Generated functional families come from `go generate ./pkg/functional`; app OpenAPI and database generation remain owned by the consuming application.

## Delivery and documentation

Fetch the default branch and edit in an isolated worktree; preserve the primary checkout. Use Conventional Commits, run affected tests plus the module build and vet checks, and open a focused PR. Report the exact verification and any skipped integration tests. Review requests produce findings and a verdict before implementation.

Update authoritative docs in the same PR when behavior, a reusable fix, a domain term or a consequential choice changes. Current behavior belongs in `docs/technical/` or its package README, verified development repairs in `docs/development/`, operational procedures in `docs/runbooks/`, and vocabulary in `CONTEXT.md`. Preserve the [dated decision log](docs/decision-log.md); new ADRs record real alternatives and consequences, and reversals supersede earlier records. Keep [the index](docs/README.md) current. `AGENTS.md` is the sole agent guide filename.
