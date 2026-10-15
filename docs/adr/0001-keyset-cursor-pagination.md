# 0001: Keyset cursor pagination for searches

- Status: accepted
- Date: 2026-10-15

## Context

`PageBuilder` paged with `LIMIT/OFFSET` over whatever `SortColumns` produced. Most sort fields are not unique (`created_at`, `name`), and nothing appended a tiebreak, so Postgres was free to return tied rows in a different order on each query. A client walking pages could skip or repeat rows even with no concurrent writes, and every insert or delete before the current offset shifted the window. Echopoint's workspace sync hit this while walking every Anchor organization and had to detect moved boundaries and restart.

## Decision

Searches page with an opaque keyset cursor.

- `search.Pagination` gains `Cursor`, and `search.Result` gains `NextCursor`. `Offset` stays for page-number navigation and cannot be combined with a cursor.
- `transactor.Keys` builds a total ordering from the requested sort fields plus a unique tiebreak (the primary key). `PageBuilder.Keyset` uses it: it fetches `limit+1` rows after the cursor's position and returns the last row's position as `NextCursor`.
- The cursor is base64url JSON with the ordering signature and one value per ordering column. A cursor from another ordering is rejected.
- Seeks follow the Postgres NULL placement (last ascending, first descending), and use a row comparison when every key is NOT NULL and shares a direction, so a composite index serves them.

## Alternatives

- **Cursor on the id only, with a forced `ORDER BY id`.** Simpler, but every search with a user-chosen sort would keep offsets, so the standard would cover only machine iteration.
- **Append an id tiebreak and keep offsets.** Fixes repeats with no writes, but inserts and deletes still shift the window, and deep offsets still scan every skipped row.
- **Signed or encrypted cursors.** A cursor carries only sort values of rows the caller already received, and scope comes from the request, so a signature would protect nothing. Rejected to keep cursors stateless and key-free.
- **Server-side cursor state (stored snapshots).** Gives snapshot consistency, but needs storage, expiry and per-replica affinity. Keyset cursors are stateless; a walk sees rows inserted ahead of it, and never repeats or skips rows that exist for the whole walk.

## Consequences

- Each repository declares its sortable columns as typed keys instead of bare columns. `SortColumns` and `OrderBy` remain for non-search queries.
- `Total` still runs a `COUNT(*)` per page; a search may skip it later without changing the cursor contract.
- Applications expose `cursor` in requests and `next_cursor` in responses, and map `search.ErrInvalidCursor` and `search.ErrCursorWithOffset` to HTTP 400.
- The property tests in `pkg/db/transactor/keyset_integration_test.go` pin the contract: for every ordering of a tie-heavy, NULL-heavy table, a cursor walk equals one ordered query, and a walk with inserts and deletes between pages returns each surviving row exactly once and in order.
