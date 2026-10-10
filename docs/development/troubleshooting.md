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

Add only observed, verified repairs; unresolved causes belong in research or the issue.
