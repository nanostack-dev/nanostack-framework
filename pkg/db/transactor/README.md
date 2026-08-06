# transactor

Context-carried SQL transaction helper for code paths that need transaction propagation across several services or repositories.

Prefer explicit transaction parameters where they keep repository boundaries clearer. Use this package when the application already relies on context propagation for transactional work.

Its two result types are built on [`pkg/functional`](../../functional): `Result[T]` wraps `functional.Result[T]` and adds SQL-specific error translation; `Optional[T]` is a type alias for `functional.Option[T]` outright, since absence has no SQL-specific behavior to add on top.

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
