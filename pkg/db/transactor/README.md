# transactor

Context-carried SQL transaction helper for code paths that need transaction propagation across several services or repositories.

Prefer explicit transaction parameters where they keep repository boundaries clearer. Use this package when the application already relies on context propagation for transactional work.

Its two result types are built on [`pkg/functional`](../../functional): `Result[T]` wraps `functional.Result[T]` and adds SQL-specific error translation; `Optional[T]` is a type alias for `functional.Option[T]` outright, since absence has no SQL-specific behavior to add on top.

## Composing transactions

`InTx` runs the callback in the transaction the context already carries, and begins one otherwise. A service composing another service's write into a larger unit calls the same method as a service that owns the transaction, and the outermost call owns the commit and the rollback:

```go
// The organization and its license are one unit; Instantiate joins rather than
// beginning a second transaction its caller's row would be invisible to.
err := tx.InTx(ctx, func(txCtx context.Context) error {
    org, err := repo.Create(txCtx, org)
    if err != nil {
        return err
    }
    return licenses.Instantiate(txCtx, licenseFor(org))
})
```

There is no way to ask for a transaction independent of the one in the context. Postgres has no such thing at this level: a second connection cannot see the first one's uncommitted rows, and blocks on the locks it holds.

## Query results

The query helpers return `Result[T]` rather than `(T, error)`. Unwrap it with `Value()`, or with `Err()` for statements that carry no value:

```go
inv, err := transactor.QueryMap(ctx, db, stmt, mapper.ToDomain).Value()
err := transactor.Exec(ctx, db, stmt).Err()
```

Type arguments are inferred from `mapFunc` — spelling them out is unnecessary.

## Queries that may find nothing

`QueryOptional`/`QueryOptionalMap` return `Optional[T]` for a SELECT that may legitimately match zero rows, or an `UPDATE`/`DELETE ... RETURNING` whose `WHERE` clause matches nothing. `IsPresent`/`Err`/`Value` answer "was there a row" and "did anything go wrong" as two separate questions, instead of folding both into a nilable pointer:

```go
result := transactor.QueryOptional[Invitation](ctx, db, stmt)
if err := result.Err(); err != nil {
    return err // a real failure
}
if !result.IsPresent() {
    return nil // no row — benign
}
inv := result.Value()
```

Chain a second Optional-producing lookup keyed by the first one's value with `FlatMap` — absence or failure from either step ends the chain the same way, without a nil-check in between:

```go
config := transactor.QueryOptional[Run](ctx, db, runStmt).
    FlatMap(func(run Run) transactor.Optional[Config] {
        return transactor.QueryOptional[Config](ctx, db, configStmtFor(run))
    })
```

## Locking the rows a query reads

A transaction does not stop another one from changing the rows it has read: under Postgres's default isolation a read never blocks a write. When the transaction depends on those rows staying as read, pass the context through `ForShare` or `ForUpdate` for that one call. Every query helper then appends the clause to its SELECT:

```go
// The license copies the template; an update to it waits until this commits,
// so the sync it starts sees the new license.
template, err := templates.Find(transactor.ForShare(txCtx), templateID)
```

- `ForShare` lets other readers take the same lock and blocks writers. `ForUpdate` blocks both. `WithRowLock` takes any go-jet clause, such as `postgres.UPDATE().NOWAIT()`.
- Pass the locked context inline, to one call. Every query run with it is locked, so assigning it to `txCtx` locks every later read in the transaction.
- A locked query outside a transaction returns `ErrRowLockOutsideTx`: the lock would end with the statement. A locked context on a statement that is not a SELECT returns `ErrRowLockNotSelect`.
- Postgres refuses a lock on an aggregate, `GROUP BY` or `DISTINCT` query, so a locked `PageBuilder` count fails.

## Paginating a search

`Page` runs the count-then-page pair behind a `search.Result`. Order a search with `Keyset`, not `OrderBy`: a keyset ends the ordering with a unique tiebreak, so every row has exactly one position, and pages are cut with a cursor rather than an offset.

```go
order := transactor.Keys(input.Sort, map[workspace.SortField]transactor.Key[model.Workspaces]{
    workspace.SortFieldCreatedAt: transactor.TimeKey(table.Workspaces.CreatedAt,
        func(m model.Workspaces) time.Time { return m.CreatedAt }),
    workspace.SortFieldName: transactor.StringKey(table.Workspaces.Name,
        func(m model.Workspaces) string { return m.Name }),
}, transactor.StringKey(table.Workspaces.ID, func(m model.Workspaces) string { return m.ID }))

return transactor.Page(r.db, r.mapper.ToDomain, table.Workspaces.AllColumns).
    From(table.Workspaces).
    Where(whereStmt).
    Keyset(order).
    Run(ctx, input.Pagination).
    Value()
```

- `Run` starts after `Pagination.Cursor` and sets `Result.NextCursor` when another row follows. An empty `NextCursor` is the last page. `Total` still counts every match of the filter.
- Offset pages get a `NextCursor` too, so a client can move from offsets to cursors at any page. A cursor with a non-zero offset fails with `search.ErrCursorWithOffset`.
- A cursor that does not decode, or was issued for another sort, fails with `search.ErrInvalidCursor`. Map both errors to HTTP 400.
- The tiebreak must be unique and NOT NULL, normally the primary key. It sorts in the direction of the last requested field.
- Pick the key constructor that matches the column: `StringKey` (text, varchar), `UUIDKey` (uuid), `EnumKey` (a Postgres enum: pass its type name and labels), `TimeKey` (timestamptz), `LocalTimeKey` (timestamp), `IntKey`, `FloatKey` (real, double precision; not numeric), and a `Nullable*` variant for each nullable column. Each rejects a cursor value its column cannot hold with `search.ErrInvalidCursor` before querying, so a forged cursor is a 400, never a database error. NULLs sort last ascending and first descending, the Postgres defaults the seek predicate follows.
- `T` must scan one SQL row into one value. A go-jet destination with a nested slice of joined children groups rows, and a join that repeats the parent multiplies them; both cut pages short. Page the parent rows flat and load children for the page in a second statement.
- Sort only on bounded columns. A cursor holds the last row's sort values and may be at most `search.MaxCursorLength` (8 KiB) long; issuing a longer one fails.
- When every key is NOT NULL and shares a direction, the seek is one row comparison, `(created_at, id) < ($1, $2)`, which a composite index on `(tenant, created_at, id)` answers with one range scan. Add that index for large tables.
- A query that does not fit `Page` uses the same `Order` directly: `OrderBy()` for the clauses, `Seek(cursor)` for the predicate, `LIMIT limit+1`, then `Trim(rows, limit)` for the page and its next cursor.

A cursor is position, not authority: it is base64url JSON, neither signed nor encrypted. Tenant scope and filters always come from the request, so an edited cursor can only move inside rows the caller may already read. See [ADR 0001](../../../docs/adr/0001-keyset-cursor-pagination.md).

## Translating constraint violations

`Result` carries the error so a driver-level constraint violation can become a domain error before the caller sees it:

```go
inv, err := transactor.QueryMap(ctx, db, stmt, mapper.ToDomain).
    OnUnique(ErrInvitationExists, "idx_platform_tenant_email").
    Value()

err := transactor.Exec(ctx, db, deleteTenant).
    OnForeignKey(ErrTenantHasProducts, "products_platform_tenant_id_fkey").
    Err()
```

`OnSQLState(code, target, constraints...)` is the general form for codes the package does not name.

Rules are no-ops when the error is nil, and the **first match wins** — once a rule replaces the driver error, later rules see a domain error and cannot match.

### Name the constraint

Passing no constraint matches *any* violation of that SQLSTATE. For `OnUnique` that includes the primary key, so an un-narrowed rule reports a duplicate generated ID — a bug — as a benign conflict. It also means a unique index added by a later migration silently widens the match, with nothing in CI to catch it.

Postgres reports the constraint *or unique index* name, so pass the index name for uniqueness declared with `CREATE UNIQUE INDEX`.

### Keep transport out of the data layer

Translation targets should be repository or domain sentinels, not HTTP faults. Mapping a sentinel to a status code belongs in the service layer.

## Integration tests

The translation rests on facts only a real database confirms — the SQLSTATE and constraint name the driver reports, and that go-jet's `fmt.Errorf("jet: %w", err)` wrapping survives `errors.As`. Those tests sit behind the `integration` build tag and skip without a DSN:

```
docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=itpass -e POSTGRES_DB=pgerrit postgres:16
PGERR_TEST_DSN="postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable" \
    go test -tags=integration ./pkg/db/transactor/
```
