# functional

Small generic value types for chaining computations that may be absent or may fail.

- `Option[T]` — a value that may legitimately be **absent**, distinct from failed.
- `Result[T]` — a value that may have **failed**. No third state.
- `Tuple2[A,B]` … `Tuple9[…]` — fixed-arity grouping, for a chain step that needs to hand more than one value to the next.

`pkg/db/transactor` builds on these: its `Optional[T]` is a type alias for `Option[T]`, and its `Result[T]` wraps `Result[T]` to add SQL error translation.

## Absent is not failed

This is the distinction the whole package exists for. A `SELECT` matching zero rows is not an error, and folding it into one (or into a nilable pointer) forces every caller to re-derive which case it is looking at.

```go
result := repo.FindConfig(ctx, id)   // Option[Config]
if err := result.Err(); err != nil {
    return err                        // a real failure
}
if !result.IsPresent() {
    return nil                        // no row — benign
}
config := result.Value()
```

`IsPresent` reports false for a failure too, so **check `Err` first** whenever the two need different handling. Every method here preserves that ordering: a failure always outranks absence and is never silently converted into it.

## Chaining

`Map` transforms a value. `FlatMap` chains a second computation that itself returns an `Option`/`Result`, so absence or failure from either step ends the chain the same way:

```go
config := transactor.QueryOptional[Run](ctx, db, runStmt).
    FlatMap(func(run Run) transactor.Optional[Config] {
        return transactor.QueryOptional[Config](ctx, db, configStmtFor(run))
    })
```

Use `FlatMap` when the later lookup is **keyed by** the earlier one's value. When the lookups are **independent**, use `ZipOptionN` instead — it combines N of them into one `Option` of a `TupleN`:

```go
pair := functional.ZipOption2(findUser(ctx, id), findTenant(ctx, tid))
if pair.IsPresent() {
    user, tenant := pair.Value().Unpack()
}
```

## Bridging Option and Result

Repositories return `Option` (absence is ordinary); services usually want an error. `ToResult` collapses the three states into two, and takes the error that absence means *here* — the repository does not get to decide that:

```go
cfg, err := repo.FindConfig(ctx, id).ToResult(ErrConfigNotFound).Value()
```

`Result.ToOption` goes the other way. Neither bridge ever replaces a real failure with the caller's sentinel.

`Get()` is the way out into ordinary Go when you are done chaining:

```go
if v, ok := opt.Get(); ok { ... }
```

## Why each type hand-rolls its own Map/FlatMap

Go has no higher-kinded types, so there is no `Functor`/`Monad` interface to implement once and share — `Option` and `Result` each need their own. What Go 1.27 *does* add is generic methods ([go.dev/issue/77273](https://go.dev/issue/77273)), so `Map`/`FlatMap` can be real methods carrying their own type parameter for the target type. That is what makes `opt.Map(f).Filter(pred).OrElse(v)` read left-to-right, instead of the nested package-level helper calls (`mo.Map(mo.Filter(opt, pred), f)`) that libraries predating generic methods still require.

## Generated code

`Tuple2`…`Tuple9` and the `ZipOption`/`ZipResult` families are **generated** — Go has no variadic generics, so each arity is a separate declaration differing only mechanically. `tuple_gen.go` and `zip_gen.go` are generated; do not edit them.

```
go generate ./pkg/functional/...
```

The generator is `internal/gen`. Change the template there, re-run, commit the result. Output is deterministic and gofmt-formatted, so a re-run on an unchanged generator produces no diff. Arity stops at 9: past that, a named struct communicates far more than a positional `Ninth` ever will.
