# Development troubleshooting

## Generic methods fail to parse

The module explicitly declares Go 1.27. Check `go version` and `go env GOTOOLCHAIN`; use the declared toolchain rather than rewriting the public API to fit an older compiler. Verify by rerunning the affected package tests. Historical toolchain/linter findings remain in the [decision log](../decision-log.md).

## Database tests pass with no database coverage

Integration tests are behind `-tags=integration` and skip when `PGERR_TEST_DSN` or `MIGRATIONS_TEST_DSN` is absent. Follow [testing](testing.md), read the test output and verify that the relevant test actually ran. A skipped suite is a verification gap.

## Fx does not resolve an adopted type

Each Go import path defines its own type identity. Check the consumer's module graph, imports and local `replace` directives for mixed legacy/framework identities. Adopt the affected injected types and their module wiring together, following the [historical adoption decisions](../decision-log.md). Verify the consumer's startup and contract suite.

## go-jet returns empty fields without an error

qrm matches destination fields by table prefix. Prefer generated model destinations; if a local destination is necessary, explicitly alias every selected column to its struct name and enable strict scanning in integration coverage. Verify the affected query against PostgreSQL rather than inferring success from an empty scan error.

## A stream stops after the server WriteTimeout

An SSE or other streaming response delivers events for the server's `WriteTimeout`, then stops. Flushes fail without an error, and a later write returns `i/o timeout`. Go applies `WriteTimeout` as an absolute connection deadline. The handler must call `http.NewResponseController(w).SetWriteDeadline(time.Time{})` before it streams. If that call returns `http.ErrNotSupported`, a middleware wrapper lacks `Unwrap() http.ResponseWriter`; the [request logger](../../pkg/httputil/requestlog/README.md#streaming-responses) implements it. Verify with a test server that sets a short `WriteTimeout` and streams past it, as in [the request log stream test](../../pkg/httputil/requestlog/stream_test.go).

## A client disconnect logs at error level

`lib/pq` reports a canceled query context as SQLSTATE `57014` "canceling statement due to user request". The error does not wrap `context.Canceled`. Log through `log.Error(ctx, err)` or `log.Event`: `log.LevelFor` uses [`pgerr.IsQueryCanceled`](../../pkg/db/pgerr/README.md) and picks warn level. A direct `.Error().Err(err)` call bypasses that rule. Verify with `go test ./pkg/log`.

## A migration fails with deadlock detected

A service exits at startup with `nanomigrate: migrate: migration failed: deadlock detected, Process A waits for ShareLock on virtual transaction ...; blocked by process B. Process B waits for AccessExclusiveLock on relation ...; blocked by process A`, usually on a `CREATE INDEX CONCURRENTLY` migration. Session B is not a migration. The index build waits for every transaction with an older snapshot to end; B holds one and then asks for an AccessExclusiveLock on the table. Postgres aborts whichever session's `deadlock_timeout` ends first, so the failure is a rare flake.

Verified cause (Anchor, 2026-11-25): a TimescaleDB retention policy job takes an AccessExclusiveLock on every table that its hypertable references with a foreign key, even with no chunk to drop (TimescaleDB 2.23 and 2.32). Anchor's migration 000030 adds the policy, the TimescaleDB scheduler starts it at once in the same run (60 ms before 000043 in a local run), and 000043 then builds an index on `organizations`, which `usage_observations` references. The deadlock was reproduced with the real job: hold `ROW EXCLUSIVE` on `organizations` in one session, start `CREATE INDEX CONCURRENTLY` on it, `CALL run_job(<retention job id>)`, wait more than `deadlock_timeout`, then commit the first session. Postgres aborts the index build with the message above and leaves the index invalid. The server log's deadlock `DETAIL` names B's query (`CALL _timescaledb_functions.policy_retention()`); the client error does not.

`nanomigrate` reruns a deadlock victim (see [pg/nanomigrate](../../pg/nanomigrate/README.md)). Verify with `go test -tags=integration -run DeadlockVictim ./pg/nanomigrate/`, which plays session B and fails without the rerun. A service on an older release fails instead, and the version stays dirty; with `auto_fix_dirty` on, the next start forces it clean, so the index stays invalid (`IF NOT EXISTS` skips it). List invalid plain indexes with `SELECT indexrelid::regclass FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid WHERE NOT i.indisvalid AND c.relkind = 'i'` and drop each one that the failed migration created with `DROP INDEX CONCURRENTLY` (both checked on the reproduction). Then, as the code of the rerun does but untested by hand, force the version before the failed migration and restart.

Add only observed, verified repairs; unresolved causes belong in research or the issue.
