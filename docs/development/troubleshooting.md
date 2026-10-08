# Development troubleshooting

## Generic methods fail to parse

The module explicitly declares Go 1.27. Check `go version` and `go env GOTOOLCHAIN`; use the declared toolchain rather than rewriting the public API to fit an older compiler. Verify by rerunning the affected package tests. Historical toolchain/linter findings remain in the [decision log](../decision-log.md).

## Database tests pass with no database coverage

Integration tests are behind `-tags=integration` and skip when `PGERR_TEST_DSN` or `MIGRATIONS_TEST_DSN` is absent. Follow [testing](testing.md), read the test output and verify that the relevant test actually ran. A skipped suite is a verification gap.

## Fx does not resolve an adopted type

Each Go import path defines its own type identity. Check the consumer's module graph, imports and local `replace` directives for mixed legacy/framework identities. Adopt the affected injected types and their module wiring together, following the [historical adoption decisions](../decision-log.md). Verify the consumer's startup and contract suite.

## go-jet returns empty fields without an error

qrm matches destination fields by table prefix. Prefer generated model destinations; if a local destination is necessary, explicitly alias every selected column to its struct name and enable strict scanning in integration coverage. Verify the affected query against PostgreSQL rather than inferring success from an empty scan error.

Add only observed, verified repairs; unresolved causes belong in research or the issue.
