# pgerr

SQLSTATE classification for PostgreSQL driver errors, so service code can map a
constraint violation to a domain error without importing `lib/pq` or matching on
message text.

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
