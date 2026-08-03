# pgerr

SQLSTATE classification for PostgreSQL driver errors, so service code can map a
constraint violation to a domain error without importing `lib/pq` or matching on
message text.

For statements issued through `pkg/db/transactor`, prefer the fluent form —
`.OnUnique(...)` / `.OnForeignKey(...)` on the returned `Result`, which is built
on this package. Use `pgerr` directly for code that does not go through those
helpers.

Use it where uniqueness cannot be settled by a pre-check. A `SELECT` before an
`INSERT` is not race-free at any isolation level — a concurrent inserter's row
stays invisible until it commits, so both callers pass the check and the loser
trips the unique index. Handling that violation is the check.

```go
created, err := repo.Create(ctx, inv)
if err != nil {
    if pgerr.IsUniqueViolation(err, "idx_platform_tenant_email") {
        return ErrInvitationAlreadyExists
    }
    return err
}
```

Constraint names stay with the caller — they are application schema knowledge.
Postgres reports the constraint *or unique index* name, so pass the index name
for uniqueness declared with `CREATE UNIQUE INDEX`.

Errors are matched with `errors.As`, so go-jet's `jet: ` wrapping and any
`fmt.Errorf("%w")` chain are unwrapped.

## Canceled queries

`IsQueryCanceled` covers a different need: telling a client-driven abort from a
server-side fault. When the context driving a query is canceled, `lib/pq` asks
Postgres to cancel the statement and reports the result as a fresh `*pq.Error`
carrying SQLSTATE `57014` — one that does not wrap `context.Canceled`, so
`errors.Is` against the context sentinels misses it.

```go
if pgerr.IsQueryCanceled(err) {
    // the caller went away; not a server fault
}
```

`57014` also covers a `statement_timeout` kill, which *is* a fault worth an
error-level log, and only the message text separates the two. `IsQueryCanceled`
matches the client-request half alone — deliberately narrower than
`Is(err, pgerr.QueryCanceled)`.
