# functional

Small generic value types for chaining computations that may be absent or may fail.

- `Option[T]` — a value that may legitimately be **absent**. No error state: a lookup that can also fail returns `(Option[T], error)`.
- `Result[T]` — a value that may have **failed**. This is the type that carries an error.
- `Validation[T]` — a value or **every** reason it could not be produced. Accumulates instead of short-circuiting.
- `Either[L,R]` — exactly one of two outcomes, where **neither side is an error**.
- `Lazy[T]` — a value computed at most once, on first access.
- `Tuple2[A,B]` … `Tuple9[…]` — fixed-arity grouping, for a chain step that needs to hand more than one value to the next.

`pkg/db/transactor` builds on these: its `Optional[T]` is a type alias for `Option[T]`, and its `Result[T]` wraps `Result[T]` to add SQL error translation.

## Absence is not failure

This is the distinction the whole package exists for. A `SELECT` matching zero rows is not an error, and folding it into one (or into a nilable pointer) forces every caller to re-derive which case it is looking at.

`Option[T]` has exactly two states — present or absent — and carries no error of its own. A computation that can also fail returns `(Option[T], error)`, the shape Go already uses for exactly this:

```go
result, err := repo.FindConfig(ctx, id)   // (Option[Config], error)
if err != nil {
    return err                            // a real failure
}
if !result.IsPresent() {
    return nil                            // no row — benign
}
config := result.Value()
```

Putting the error in Go's own return position, rather than inside the `Option` struct, is not a style preference. An error stored in a struct field is invisible to `errcheck`, invisible to `go vet`, and invisible to the compiler — nothing stops a caller from reading `result.Value()` and never noticing the field was set. Returning `(Option[T], error)` puts the failure where Go's tooling already knows to look for it: a caller who drops the `error` return gets flagged by errcheck exactly as if they had dropped any other error. `Option` answers one question — is a value here — and answers it honestly every time, because there is nowhere else for a failure to hide.

## Chaining

`Map` transforms a value. `FlatMap` chains a second computation that itself returns an `Option`, so absence from either step ends the chain the same way:

```go
config := someOption.FlatMap(func(run Run) functional.Option[Config] {
    return lookupConfig(run) // a lookup that cannot fail
})
```

But `FlatMap`'s `f` must return `Option[R]`, not `(Option[R], error)`, so it cannot express a second step that can itself fail — which most repository lookups can. Chain those sequentially instead, resolving the error between the two calls:

```go
run, err := transactor.QueryOptional[Run](ctx, db, runStmt)
if err != nil || !run.IsPresent() {
    return functional.None[Config](), err
}
return transactor.QueryOptional[Config](ctx, db, configStmtFor(run.Value()))
```

That is more lines than a single chained call, and that is the point: the error a three-state `Option` used to carry invisibly inside itself is now sitting in plain sight at both call sites, exactly where `errcheck` and a reviewer both know to look for it.

Reach for `FlatMap` only when the second Option-producing step genuinely cannot fail. When two lookups are independent — neither is keyed by the other's value — use `ZipOptionN` instead — it combines N of them into one `Option` of a `TupleN`:

```go
pair := functional.ZipOption2(findUser(ctx, id), findTenant(ctx, tid))
if pair.IsPresent() {
    user, tenant := pair.Value().Unpack()
}
```

## Bridging Option and Result

Repositories return `Option` (absence is ordinary); services usually want an error. `ToResult` converts absence into the error that absence means *here* — the repository does not get to decide that:

```go
cfg, err := repo.FindConfig(ctx, id).ToResult(ErrConfigNotFound).Value()
```

There is no bridge the other way, from `Result` to `Option`. With only two states, an `Option` has nowhere to put a `Result`'s error except by discarding it, and a conversion whose only job is throwing information away is not one this package offers. If a failure should be treated as absence, that is a decision for the call site to make explicitly — not a method that makes it silently for every caller.

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

`ToOption` maps a Left to `None`, for exactly that reason — a Left is not a failure, so there is nothing to carry forward but absence. Use `Fold` or `ToResult(func(L) error)` when the Left payload must survive.

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

`ToPtr` turns absence into `nil` — the same shape a generated OpenAPI struct already expects for an optional field. `Option` carries no error, so there is nothing this bridge could lose: the two states on either side of it line up exactly.

## Why each type hand-rolls its own Map/FlatMap

Go has no higher-kinded types, so there is no `Functor`/`Monad` interface to implement once and share — `Option` and `Result` each need their own. What Go 1.27 *does* add is generic methods ([go.dev/issue/77273](https://go.dev/issue/77273)), so `Map`/`FlatMap` can be real methods carrying their own type parameter for the target type. That is what makes `opt.Map(f).Filter(pred).OrElse(v)` read left-to-right, instead of the nested package-level helper calls (`mo.Map(mo.Filter(opt, pred), f)`) that libraries predating generic methods still require.

## Generated code

`Tuple2`…`Tuple9` and the `ZipOption`/`ZipResult`/`ZipValidation` families are **generated** — Go has no variadic generics, so each arity is a separate declaration differing only mechanically. `tuple_gen.go`, `zip_gen.go` and `validation_gen.go` are generated; do not edit them.

```
go generate ./pkg/functional/...
```

The generator is `internal/gen`. Change the template there, re-run, commit the result. Output is deterministic and gofmt-formatted, so a re-run on an unchanged generator produces no diff. Arity stops at 9: past that, a named struct communicates far more than a positional `Ninth` ever will.
