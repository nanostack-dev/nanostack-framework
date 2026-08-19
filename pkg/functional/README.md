# functional

Small generic value types for chaining computations that may be absent or may fail.

- `Option[T]` — a value that may legitimately be **absent**, distinct from failed.
- `Result[T]` — a value that may have **failed**. No third state.
- `Validation[T]` — a value or **every** reason it could not be produced. Accumulates instead of short-circuiting.
- `Either[L,R]` — exactly one of two outcomes, where **neither side is an error**.
- `Lazy[T]` — a value computed at most once, on first access.
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

## Validation accumulates where Result short-circuits

`Result` stops at the first error. That is right for a pipeline where step two needs step one's value, and wrong for a form: a user who submitted four bad fields must be told about all four.

```go
v := functional.ZipValidation3(validateName(in), validateEmail(in), validateAge(in))
if !v.IsValid() {
    return v.Err()          // errors.Join of every failure, in argument order
}
name, email, age := v.Value().Unpack()
```

`Validation` deliberately has **no `FlatMap`**. `FlatMap` is sequential — `f(a)` cannot run until `a` exists — so a chain of them can only ever report the first failure, which is the one thing the type exists to avoid. Accumulation happens through `ZipValidationN` instead, where all N inputs already exist independently.

This is narrower than Vavr's `Validation`, which carries both a short-circuiting `flatMap` and an accumulating `ap`/`combine` and leaves the choice to the caller. Offering both on one type is a trap: reaching for `FlatMap` silently drops back to first-error reporting.

`Err()` joins the accumulated errors with `errors.Join`, so a `Validation` leaves the package as an ordinary Go error and every individual failure stays `errors.Is`-matchable.

## Either is not the error type

`Either[L,R]` is right-biased — `Map` and `FlatMap` act on the Right, and a Left passes through untouched — following Scala 2.12+ and Vavr.

Reach for it only when **neither side is a failure**: a cache hit versus a fresh fetch, a discriminated union off a webhook payload. When one side *is* an error, `Result[T]` is the type — it is `Either[error, T]` with the Left already fixed, which is why Scala right-biased `Either` and deprecated its projections in the first place.

`ToOption` maps a Left to `None`, not `Failed`, for exactly that reason. Use `Fold` or `ToResult(func(L) error)` when the Left payload must survive.

## Lazy, and when to prefer sync.OnceValue

`Lazy[T]` computes at most once, on first `Get`, and is safe under concurrent access.

```go
cfg := functional.NewLazy(loadExpensiveConfig)
label := cfg.Map(func(c Config) string { return c.Name })   // neither forced yet
```

`Map` stays lazy in both directions and memoizes its own result. That is the difference from `sync.OnceValue`, which memoizes one call and hands back a `func() T`: composing those re-runs the outer function on every read. Use `sync.OnceValue` when nothing derives from the value, and `Lazy` when something does.

## Bridging to ordinary Go

Generated OpenAPI structs use pointers for optional fields, so the pointer bridges matter most in practice:

```go
opt := functional.FromPtr(req.Description)      // *string  → Option[string]
req.Description = opt.ToPtr()                   // Option[string] → *string
v, ok := m[key]; o := functional.OptionOf(v, ok)
```

`ToPtr` flattens a failure to `nil` — a `*T` has no error channel, so this is the one bridge where failure and absence become indistinguishable. Check `Err` before crossing it.

## Why each type hand-rolls its own Map/FlatMap

Go has no higher-kinded types, so there is no `Functor`/`Monad` interface to implement once and share — `Option` and `Result` each need their own. What Go 1.27 *does* add is generic methods ([go.dev/issue/77273](https://go.dev/issue/77273)), so `Map`/`FlatMap` can be real methods carrying their own type parameter for the target type. That is what makes `opt.Map(f).Filter(pred).OrElse(v)` read left-to-right, instead of the nested package-level helper calls (`mo.Map(mo.Filter(opt, pred), f)`) that libraries predating generic methods still require.

## Generated code

`Tuple2`…`Tuple9` and the `ZipOption`/`ZipResult`/`ZipValidation` families are **generated** — Go has no variadic generics, so each arity is a separate declaration differing only mechanically. `tuple_gen.go`, `zip_gen.go` and `validation_gen.go` are generated; do not edit them.

```
go generate ./pkg/functional/...
```

The generator is `internal/gen`. Change the template there, re-run, commit the result. Output is deterministic and gofmt-formatted, so a re-run on an unchanged generator produces no diff. Arity stops at 9: past that, a named struct communicates far more than a positional `Ninth` ever will.
