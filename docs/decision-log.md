# Decision Log

## 2026-05-22: Framework Vocabulary

Use `app/`, `modules/`, `pkg/`, `gen/`, `cli/`, `starter/`, and `docs/` as the framework vocabulary.

Avoid `runtime`, `foundation`, `templates`, `shared`, `common`, `utils`, and `toolkit` as top-level identities because they either obscure boundaries or suggest an unbounded bucket.

## 2026-05-22: History Strategy

Start `nanostack-framework` from clean framework history rather than importing old `nanostack-shared` history by default.

Rationale: old shared history contains personal author metadata that should not become part of the framework's public identity unless explicitly accepted.

## 2026-05-22: First App Adoption Slice

Adopt `pkg/health` from `anchor` and `echopoint` as the first framework package consumed by both services.

Rationale: health is a low-risk primitive with no product semantics. It validates module wiring, local replaces, and compatibility with both CT suites before migrating deeper packages.

## 2026-05-22: Request Logging Adoption Slice

Adopt `pkg/httputil/requestlog` from `anchor` and `echopoint` while keeping each app's local middleware wrapper responsible for skipping health checks.

Rationale: request logging is framework-owned HTTP shell behavior, but each service may still decide which routes are too noisy to log.

## 2026-05-22: FX Module Adoption Slice

Move `anchor` and `echopoint` off `shared/fxmodules` for config, logging, postgres, cache, migrations, and pglock imports.

Rationale: these modules share injected types, especially the config loader and cache interface, so adopting them as one slice avoids mixed DI graphs with duplicate package identities.

## 2026-05-22: Search Contract Split

Move shared search request/result contracts to `pkg/search` and keep Jet-specific query helpers in `pkg/jetx`.

Rationale: the search request model is reused across API, domain, service, repository, tests, and generated code, but Jet helper functions are persistence-specific. Splitting them keeps the public contract useful without dragging database concerns into every import site.

## 2026-05-22: Low-Risk Helper Slice

Keep language-level pointer and slice helpers in narrow framework packages: `pkg/ptr` and `pkg/slicex`.

Rationale: `Ptr`, `DerefOr`, slice mapping, and string-diff helpers are broadly reused, but they do not justify another unbounded `toolkit`-style bucket. `CastToStringPtr` stays app-local where it is only needed by a single feature mapper.

## 2026-05-22: ID Prefix Compatibility

Allow underscores in `pkg/ids` prefixes.

Rationale: existing Anchor and Echopoint public IDs already use underscore prefixes such as `product_apikey` and `organization_apikey`. Framework adoption should preserve those IDs instead of forcing a rename-only migration.

## 2026-05-23: API Error Adoption Boundary

Adopt `pkg/apierror` first at the HTTP middleware boundary while continuing to accept legacy `toolkit.NanostackError` values as inputs.

Rationale: middleware is the smallest slice where the framework can own response writing and status handling without forcing a repo-wide replacement of service-layer error constructors, validators, and Nanostack client helpers in the same change.

## 2026-05-23: Strict HTTP Error Rendering

Move default strict-handler request and response error rendering into `modules/httpserver`, while keeping legacy error adaptation and SSE route detection as app-owned hooks.

Rationale: strict OpenAPI handlers were repeating the same JSON error writing and DTO conversion logic in each service. Keeping the reusable writer in the HTTP shell removes app-local conversion code without forcing `nanostack-framework` to import `nanostack-shared/toolkit` or guess each app's streaming routes.

## 2026-05-23: Default Validator Helper

Expose package-level `pkg/validate.ValidateStruct` backed by a framework-owned default validator, while keeping `NewStructValidator()` available for explicit construction.

Rationale: most service-layer validation call sites only need a stable, shared validator instance and an `error` return they can propagate. Providing a package-level helper enables small migration slices away from `toolkit.ValidateStruct` without pulling validator construction into every feature package.

## 2026-05-25: Context Transactor Helper Surface

Expose the shared Jet query helpers on `pkg/db/transactor` and provide the FX binding from `modules/transactor`.

Rationale: Anchor and Echopoint already migrated repository code to the context-carried transaction package, but the published framework tag only exposed the transaction carrier itself. Keeping the query helpers and FX module in the same framework package avoids app-local wrappers and preserves a single transaction context identity across services.

## 2026-07-27: Constraint-Violation Classification

Expose `pkg/db/pgerr` for SQLSTATE classification, with constraint names supplied by the caller.

Rationale: five repository and service call sites across Anchor and Echopoint had each hand-rolled the same `errors.As` unwrap to `*pq.Error` plus SQLSTATE and constraint comparison, pulling a driver import into service packages that otherwise have none. The unwrap is driver knowledge and belongs in the framework; the constraint names are application schema and stay in the apps. The classification is needed because a pre-insert existence check is not race-free at any isolation level, so the constraint violation is the only correct answer to "does this already exist".

## 2026-07-27: Query Helpers Return `Result[T]`

Change the `pkg/db/transactor` query helpers from `(T, error)` to `Result[T]`, unwrapped with `Value()` or `Err()`, and hang `OnUnique`/`OnForeignKey`/`OnSQLState` off it.

Rationale: constraint translation is only useful at the call site that issued the statement, and a free-function form (`pgerr.Map(err, ...)`) needs a second statement plus a temporary. Methods cannot declare their own type parameters, so a reusable mapper cannot wrap a `(value, error)` pair generically — but methods on a generic type can, which makes `Result[T]` the only shape that expresses the rule inline. Type inference already resolves the helpers' type arguments from `mapFunc`, so the fluent call site is no longer than the tuple form it replaces.

Cost accepted: this is a breaking change for every caller, which must add `Value()` or `Err()`, and the error is unobserved by `errcheck` until the terminal call. `pgerr` remains usable directly for code that does not go through these helpers.

## 2026-07-27: Single Query Entry Point

Remove the `jetx` query helpers (`Query`, `QueryOptional`, `QueryMap`, `QueryOptionalMap`, `QueryMapSlice`, `Exec`, `QueryCount*`, `WithTx`, `WithTxReturn`, `Executor`, `DBOptions`, `CountResult`). `pkg/db/transactor` is the only way to run a statement; `jetx` keeps ordering, expression conversion, and filter composition.

Rationale: the two sets were near-identical, differing only in how a transaction was supplied — `jetx` took an explicit `*DBOptions{Tx}`, `transactor` carries it in context. The 2026-05-25 decision already made `transactor` the shared surface, but the `jetx` twin stayed exported and unused. Once query results became `Result[T]`, keeping it meant one of the two ways to query silently bypassed constraint translation, so a repository could reintroduce an unhandled 23505 by picking the wrong helper. Nothing referenced the removed symbols — in the framework, Anchor, Echopoint, echopoint-runner, or pgkit — so the removal has no migration. Dropping them also breaks `jetx`'s dependency on `transactor`.

## 2026-07-27: Typed Cache Is The Cache

Make the generic `cache.Cache[T]` the API applications use, rename the untyped backend interface to `cache.Store`, and delete the `interface{}`-based struct methods (`GetStruct`, `SetStruct`, `GetOrElseStruct`, `GetOrElseStructWithExpiry`).

Rationale: the untyped surface predated generics, and applications never wanted it — each wrapped it in a hand-written typed service to get real types back. Anchor had two such services (262 lines) differing only in entity type and key format; Echopoint had a third inline. Keeping both surfaces would leave the typed one looking like an optional extra layered on the "real" API, when the dependency runs the other way: `Cache[T]` is what callers want, and `Store` is the string-valued backend it happens to sit on. `Cache[T]` now owns serialization directly over `Store`'s string methods, so there is one way to cache a value and it is type-safe. It is a generic type rather than methods on `Store`, because Go methods cannot declare their own type parameters.

## 2026-07-28: Paginated Search Execution

Add `pkg/db/transactor.Page` and `SortColumns` for the count-then-page pair behind a `search.Result`.

Rationale: fifteen repository searches across Anchor and Echopoint hand-wrote the same skeleton — count, select a page, order, limit/offset, map, assemble — for about 1175 lines, of which only the filter predicates and the sortable columns are per-entity. `Page` owns the rest and returns a `Result`, so it terminates with `Value()` and the `On*` translations compose. `SortColumns` replaces the `switch sort.Field` block nine of them carried; a field-to-column mapping is data, and as a map a missing case cannot fall through unnoticed.

Both type parameters bind at construction, inferred from `mapFunc`, so no call site spells one. That is also why `mapFunc` precedes the columns: a method cannot introduce a type parameter, so anything the builder needs must arrive before the chain starts. `SortColumns` is a free function for the same reason — it introduces the sort-field type.

It does not cover every search. `product_role_repository.SearchByProductID` pages over role IDs and then fetches permissions separately, because paginating the joined rows would truncate a role's permissions; that stays hand-written. Searches doing per-item work after the fetch keep their loop after `Value()`.

## 2026-07-30: Security Requirements Come From The Contract

Add `pkg/apisec` and `modules/apisec`. An operation's OpenAPI `security` block is resolved from the document at request time and evaluated there, replacing oapi-codegen's generated `<Scheme>Scopes` request-context keys.

Rationale: the generated mechanism flattens the requirement list. It records which schemes an operation names and the scopes each carries, but not how they combine, so alternative schemes (OR), combined schemes (AND) and an anonymous `{}` alternative are indistinguishable once written into the context. The flattening happens in oapi-codegen's own data model — `DescribeSecurityDefinition` reduces `openapi3.SecurityRequirements` to a flat `[]SecurityDefinition` before any template runs — so overriding a template cannot recover the structure. Upstream deprecated the feature rather than fix it (oapi-codegen#1524) and now hides it behind `compatibility.enable-auth-scopes-on-context`. It matters here because 83 of Echopoint's 98 operations and 33 of Anchor's 84 declare two alternative schemes, and both applications had rebuilt the disjunction by hand — asking "does this operation accept bearer auth?" by testing whether a context key happened to exist.

`Evaluate` takes an `Authenticator` returning `(context.Context, error)` rather than an error alone. That is the difference from `openapi3filter.AuthenticationFunc`, which the same problem would otherwise suggest: it returns only an error, and `nethttp-middleware` then calls the next handler with the unmodified request, so a scheme that verified a caller has nowhere to put the principal, tenant, or product scope it resolved. Threading the context lets applications keep credential verification, the context they derive from it, and their own error responses, while the disjunction and conjunction are evaluated once, here.

`ErrSchemeNotAttempted` separates "this credential is absent" from "this credential was rejected". Under a disjunction every alternative is tried, so without the distinction the error surfaced to a client could belong to a scheme it never used. Failures carrying it are reported after real rejections for that reason.

The resolver reports an unmatched route as unmatched rather than as an empty requirement set. The two are not the same: an empty set means the operation declares no security and is public, so collapsing them would make an unroutable request look unrestricted.

## 2026-08-19: Generic Methods Are Legal On Go 1.27

Go 1.27 lifts the restriction cited in the 2026-07-27 "Query Helpers Return `Result[T]`", 2026-07-27 "Typed Cache Is The Cache", and 2026-07-28 "Paginated Search Execution" entries: methods can now declare their own additional type parameters. That is what `pkg/functional`'s `Option[T].Map[R]` and `FlatMap[R]` needed to exist as real methods instead of package-level functions, and why the package was pulled from main until the toolchain shipped (see the revert commit this change reverses). `go.mod` now declares `go 1.27` with `toolchain go1.27.0`, replacing the `go1.27rc1` pin the branch carried while the release was still a candidate.

Design choices made under the old constraint are not automatically wrong now — `Result[T]` as a generic type rather than methods on a non-generic `Store`, for instance, still stands on its own reasoning — but the constraint itself is gone and no longer needs working around in new code.

Cost accepted: `golangci-lint` cannot analyse this package yet. The whole-program IR and SSA builders that several linters sit on do not terminate on a generic method whose result type re-instantiates its own receiver. `honnef.co/go/tools@v0.7.0` overflows the stack in `ir.(*Program).needMethods`, and with `honnef.co/go/tools@v0.8.0-rc.1` swapped in, `gosec` then panics in `x/tools`' `typesinternal.ForEachElement` with the type-parameter name. Both are upstream bugs, not findings about this code. Lint stays disabled for the affected repositories until a fixed release exists; no staticcheck-family linter is disabled and no suppression is added in the meantime.

Rationale for recording this here rather than relying on model training data: this is a language change newer than most models' training cutoff, so it will not be "known" context without an explicit note — this entry is that note.

## 2026-08-19: Validation Accumulates, Either Is Not The Error Type

Expand `pkg/functional` with `Validation[T]` (accumulating), `Either[L, R]` (right-biased), `Lazy[T]` (memoized), and the combinators the existing `Option`/`Result` were missing — `Fold`, `Or`, `Peek`, `Recover`, `RecoverWith`, `Try`, `Filter`, and the pointer bridges `FromPtr`/`ToPtr`/`OptionOf`. `ZipValidation2`..`ZipValidation9` join the generated families.

Rationale: `Result` short-circuits, which is correct for a pipeline whose second step needs the first step's value and wrong for a request with four bad fields. `Validation` is the accumulating twin, and it deliberately omits `FlatMap` — a sequential combinator can only ever report the first failure, which is the single reason to reach for the type. This is narrower than Vavr, which carries both a short-circuiting `flatMap` and an accumulating `ap`/`combine` on one type; offering both makes the accumulating behaviour opt-out by accident.

`Either` is deliberately not the error type. `Result[T]` already is `Either[error, T]` with the left side fixed, which is what Go call sites want — Scala reached the same place when it right-biased `Either` in 2.12 and deprecated the projections. `Either` here is for a genuine two-outcome branch where neither side is a failure, and `ToOption` maps a Left to `None` rather than `Failed` to keep that honest.

`Lazy` is not `sync.OnceValue` restated. `OnceValue` memoizes one call and returns a `func() T`, which composes only by wrapping, so a derived value is recomputed on every read. `Lazy.Map` returns a `Lazy` that memoizes its own result, so a chain costs one evaluation per link. Where nothing derives from the value, `sync.OnceValue` remains the right tool.

Not ported, and the reason: persistent collections and `Stream` (`pkg/slicex`, `slices`, `maps`, and `iter.Seq` own that ground), `Future`/`Promise` (goroutines, channels, `errgroup`), pattern matching, and `Function0..8` currying. Those exist in Vavr because Java lacks first-class function types and cheap concurrency. Porting them would produce a package this codebase would not import.

Cost accepted: hand-written statement coverage is complete, but the generated mid-arity combinators (4 through 8) are exercised only by construction — the tests cover arities 2, 3 and 9, so package coverage reads 74.1%. The lint blocker recorded in the entry above is unchanged and now covers more code.

## 2026-08-19: One Name For The Absent Value

Remove `transactor.Optional[T]`, the type alias for `functional.Option[T]`. `QueryOptional` and `QueryOptionalMap` now return `functional.Option[T]` directly, and `pkg/db/transactor/optional.go` is deleted.

Rationale: the alias added a second name for one type and nothing else. It was introduced when `pkg/functional` was new and the SQL layer wanted a local vocabulary, but the parallel case does not hold — `transactor.Result[T]` is a distinct type that wraps `functional.Result[T]` to add SQL error translation, whereas `Optional` had no rules of its own to add. Two names for one type make a reader ask what the difference is, and the honest answer was "none".

Removing it rather than deprecating it is deliberate. The alias is transparent, so both spellings compile against either version and a deprecation period would leave the ambiguity in place for as long as anyone tolerated the warning. Consumers adopt `functional.Option[T]` when they take the version bump, which is the moment they are already reading the diff.

Cost accepted: this is a breaking change. Anchor has two call sites, both in `integration_instance_repository.go`; Echopoint, echopoint-runner and pgkit have none. The fix is mechanical — `transactor.Optional[T]` becomes `functional.Option[T]` with the import that implies.
