# 0015: Fork go-jet as the db module (multi-dialect)

- Status: proposed
- Date: 2026-11-07

## Context

Services build SQL with `go-jet/jet` v2 through `pkg/jetx`, `pkg/search` and `pkg/db/transactor`, about 396 call sites. jet gives compile-checked SQL: every expression is typed, so `IntegerExpression.EQ` refuses a string expression at compile time. Its weak spots sit around the builder, not in it:

- `qrm` scans results by reflection (117 `reflect` lines in 11 files). A column that does not match the destination leaves a silent zero value unless `StrictScan` is on.
- `qrm.Queryable` returns a concrete `*sql.Rows`, so jet runs only on `database/sql`. There is no pgx executor and no transaction API, and the query logger is process-global.
- The write side is untyped: `MODEL` panics on a mismatch. A second `WHERE` replaces the first, and an empty `AND()` or `OR()` panics.
- The generator emits tables, models and enums only, and hard-codes nullable columns as `*T`.
- Upstream has a bus factor of one (744 of about 944 commits) and two to three releases a year.

The framework must keep PostgreSQL, MySQL/MariaDB and SQLite, so that no product is locked to one database.

## Decision

The framework forks go-jet v2.16.1 (Apache-2.0) and makes it its `db` module. It is the framework's only database library.

- All three dialects stay: `db` (jet's `internal/jet` core, made public), `db/pg`, `db/mysql` and `db/sqlite`. CockroachDB is dropped.
- `db/exec` defines an `Executor` over an abstract `Rows`, with a pgx implementation and a `database/sql` implementation (for MySQL and SQLite). The transactor moves into `db/exec` and keeps the context-carried transaction (`InTx`).
- `nano gen db` (in the `gen` module, at `gen/db`, with pure-Go introspection drivers) generates one mapper per destination type. Mappers replace `qrm`'s reflection; a column mismatch is an error that lists the missing and extra aliases. `qrm` is deleted once every call site uses mappers.
- The fork adds typed writes (generated setters with `Option` fields), appending `WHERE`, typed `IN`, `Option[T]` as the default nullable strategy, context hooks and executor decorators. Later stages add relationships, typed loaders, factories for the test module and constraint-named errors. These designs come from bob (MIT); ported template code keeps bob's notice.
- The runtime stays driver-free. `db` depends on pgx only; a product adds its own MySQL or SQLite driver.
- Every change to `db`, `db/exec` or `gen/db` must pass the PostgreSQL, MySQL/MariaDB and SQLite CI jobs before it merges. jet's per-dialect suites move from docker-compose to testcontainers.
- The work runs in four stages, and each can stop safely: mappers as a sidecar on unmodified jet; the fork and the executor seam; typed writes and the other gaps; relationships, loaders, factories and errors.

## Alternatives

- **bob v0.50.0 (MIT).** It already ships generated scan mappers, a pgx adapter, factories, hooks and typed setters, so it scores higher on most dimensions. Those are cost gaps that can be added to jet without changing its API. bob's missing compile-checked expression hierarchy is a ceiling gap: its operators and query mods take `any` or `bob.Expression`, and typing them means redesigning its public API in three dialects, which ends upstream merges. It also brings about 39.9k lines plus templates (against about 20.3k for jet), three SQL parser stacks, 30 direct dependencies in one `go.mod`, an external `scan` library that still reflects per column, and 17 breaking entries in its last two releases.
- **Keep go-jet unforked.** The executor seam, generated mappers and typed writes all change jet's core (`internal/jet/statement.go`, `qrm`, the generator). Upstream has no pgx executor (only an unmerged draft), and a single maintainer decides what lands.
- **sqlc.** SQL files in, typed functions out. Queries are static, so the dynamic filters and keyset pagination in `pkg/search` and the transactor would be string building again, and it has no expression builder to compose.
- **ent.** A schema-first ORM with its own graph model and code generator. It replaces SQL with its API, and every product would move its schema into ent's DSL.
- **A PostgreSQL-only library.** Simpler, but it locks every product to one database, which the framework rules out.

## Consequences

- The framework carries the fork alone. It keeps `LICENSE`, `NOTICE` and the copyright lines, and marks modified files. Each quarter it reviews jet releases and cherry-picks dialect and expression fixes only, since `qrm`, the executor and the generator diverge on purpose.
- Products move from `lib/pq` to pgx during the executor stage.
- A column mismatch becomes an error instead of a silent zero value.
- Three dialects make every `db` change pay for three CI jobs.
- [ADR 0020](0020-framework-owned-db-migrate.md) (`db/migrate`) and the test module's template databases follow the same three dialects.
- The decision-log entries that shape `pkg/search` (2026-05-22, "Search Contract Split") and make `pkg/db/transactor` the single query entry point (2026-07-27) stay valid; those packages move into `db` with the fork.
- The keyset pager and the search runtime move into `db` before `db/exec` exists, as `db/search`, `db/keyset` and `db/searchexec` ([ADR 0027](0027-search-in-the-db-module.md)). They take the executor as a parameter, typed `query.Executor` (an alias of `qrm.Queryable`), and run every statement through `db/query`. The context transaction stays in `pkg/db/transactor` until stage 2. So stage 2 changes that one alias and one scan path, and the pager does not move again.
