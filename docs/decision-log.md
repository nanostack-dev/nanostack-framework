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

Superseded by the 2026-08-21 entry below: `pkg/slicex` is deleted and `functional.Seq` owns slice transformation. `pkg/ptr` stands.

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

Cost accepted (superseded by the 2026-08-19 "The Lint Blocker Was A Stale Dependency" entry): `golangci-lint` cannot analyse this package yet. The whole-program IR and SSA builders that several linters sit on do not terminate on a generic method whose result type re-instantiates its own receiver. `honnef.co/go/tools@v0.7.0` overflows the stack in `ir.(*Program).needMethods`, and with `honnef.co/go/tools@v0.8.0-rc.1` swapped in, `gosec` then panics in `x/tools`' `typesinternal.ForEachElement` with the type-parameter name. Both are upstream bugs, not findings about this code. Lint stays disabled for the affected repositories until a fixed release exists; no staticcheck-family linter is disabled and no suppression is added in the meantime.

Rationale for recording this here rather than relying on model training data: this is a language change newer than most models' training cutoff, so it will not be "known" context without an explicit note — this entry is that note.

## 2026-08-19: Validation Accumulates, Either Is Not The Error Type

Expand `pkg/functional` with `Validation[T]` (accumulating), `Either[L, R]` (right-biased), `Lazy[T]` (memoized), and the combinators the existing `Option`/`Result` were missing — `Fold`, `Or`, `Peek`, `Recover`, `RecoverWith`, `Try`, `Filter`, and the pointer bridges `FromPtr`/`ToPtr`/`OptionOf`. `ZipValidation2`..`ZipValidation9` join the generated families.

Rationale: `Result` short-circuits, which is correct for a pipeline whose second step needs the first step's value and wrong for a request with four bad fields. `Validation` is the accumulating twin, and it deliberately omits `FlatMap` — a sequential combinator can only ever report the first failure, which is the single reason to reach for the type. This is narrower than Vavr, which carries both a short-circuiting `flatMap` and an accumulating `ap`/`combine` on one type; offering both makes the accumulating behaviour opt-out by accident.

`Either` is deliberately not the error type. `Result[T]` already is `Either[error, T]` with the left side fixed, which is what Go call sites want — Scala reached the same place when it right-biased `Either` in 2.12 and deprecated the projections. `Either` here is for a genuine two-outcome branch where neither side is a failure, and `ToOption` maps a Left to `None` rather than `Failed` to keep that honest.

`Lazy` is not `sync.OnceValue` restated. `OnceValue` memoizes one call and returns a `func() T`, which composes only by wrapping, so a derived value is recomputed on every read. `Lazy.Map` returns a `Lazy` that memoizes its own result, so a chain costs one evaluation per link. Where nothing derives from the value, `sync.OnceValue` remains the right tool.

Not ported, and the reason: persistent collections and a lazy `Stream` (`functional.Seq`, `slices`, `maps`, and `iter.Seq` own that ground), `Future`/`Promise` (goroutines, channels, `errgroup`), pattern matching, and `Function0..8` currying. Those exist in Vavr because Java lacks first-class function types and cheap concurrency. Porting them would produce a package this codebase would not import.

Cost accepted: hand-written statement coverage is complete, but the generated mid-arity combinators (4 through 8) are exercised only by construction — the tests cover arities 2, 3 and 9, so package coverage reads 74.1%. The claim here that the lint blocker is unfixable is superseded by the 2026-08-19 "The Lint Blocker Was A Stale Dependency" entry.

## 2026-08-19: One Name For The Absent Value

Remove `transactor.Optional[T]`, the type alias for `functional.Option[T]`. `QueryOptional` and `QueryOptionalMap` now return `functional.Option[T]` directly, and `pkg/db/transactor/optional.go` is deleted.

Rationale: the alias added a second name for one type and nothing else. It was introduced when `pkg/functional` was new and the SQL layer wanted a local vocabulary, but the parallel case does not hold — `transactor.Result[T]` is a distinct type that wraps `functional.Result[T]` to add SQL error translation, whereas `Optional` had no rules of its own to add. Two names for one type make a reader ask what the difference is, and the honest answer was "none".

Removing it rather than deprecating it is deliberate. The alias is transparent, so both spellings compile against either version and a deprecation period would leave the ambiguity in place for as long as anyone tolerated the warning. Consumers adopt `functional.Option[T]` when they take the version bump, which is the moment they are already reading the diff.

Cost accepted: this is a breaking change. Anchor has two call sites, both in `integration_instance_repository.go`; Echopoint, echopoint-runner and pgkit have none. The fix is mechanical — `transactor.Optional[T]` becomes `functional.Option[T]` with the import that implies.

## 2026-08-19: The Lint Blocker Was A Stale Dependency

Correction. The two entries above record that `golangci-lint` cannot analyse `pkg/functional`, and that nothing could be done but wait for upstream. The first half is true of a stock `golangci-lint`. The second half is wrong: the fix already existed and had done for weeks.

`golangci-lint` v2.12.2 pins `golang.org/x/tools` at a pseudo-version dated 2026-04-20. The Go team repaired the underlying defect after that date, in [golang/go#80055](https://github.com/golang/go/issues/80055) (CL 786280, CL 792260), which surfaced publicly when govulncheck hit the same panic in [golang/go#80139](https://github.com/golang/go/issues/80139). Latest `x/tools` is v0.49.0.

The defect itself is one broken assumption. `x/tools/internal/typesinternal/element.go` asserts `panic(T)` on a `*types.TypeParam`, guarded by the comment "forEachReachable must not be called on parameterized types". Before Go 1.27 that held: the method set of an instantiated type could not contain an uninstantiated type parameter. Generic methods break it — `Option[int]`'s method set contains `Map[R]`, whose signature names an uninstantiated `R`. Hence the panic value is literally `R`. honnef's IR fails on the same premise by a different route: `needMethods` sees `Map[R] -> Option[R]`, enumerates that, finds `Map[R2] -> Option[R2]`, and recurses without bound because every instantiation is a distinct `*types.Named`.

The compiler is unaffected because it instantiates on demand, from actual call sites, which is a finite set. Whole-program linters enumerate every reachable runtime type to build a call graph, and with generic methods that set is infinite.

Three linters are affected across two independent toolchains: `gosec` and `unparam` through `x/tools` SSA, and the staticcheck family through honnef's IR. Disabling the staticcheck family alone does not help — `unparam` panics identically.

Both dependencies must move. Verified against this repository: `x/tools` v0.49.0 alone still hangs, honnef v0.8.0-rc.1 alone still panics, and the two together complete in 80 seconds with real findings. `pkg/functional` is now clean; the repository-wide count is 85, all pre-existing and outside this package.

No linter is disabled and no suppression is added. Every finding in `pkg/functional` was repaired by changing the code — including `TryRecover`, which lost its named return to an inner closure, and the generator's package-level lookup tables, which moved into `newArity`.

Rationale for recording a correction rather than editing the entries above: the log is a dated record of what was decided and believed at the time. Rewriting it would hide that the wrong conclusion was drawn and acted on. The two entries are marked as superseded and left standing.

## 2026-08-20: Option Drops The Error State

`Option[T]` goes from three states to two: present and absent. `Failed[T]` and `(Option[T]).Err()` are removed, along with `(Result[T]).ToOption()` and `(Validation[T]).ToOption()` — both existed only to fold an error into the state a two-state `Option` no longer has, and both could do nothing but discard it. `transactor.QueryOptional` and `QueryOptionalMap` now return `(functional.Option[T], error)` instead of `functional.Option[T]`, and `ZipOption2`..`ZipOption9` lose their error branch.

This supersedes the 2026-08-19 "Generic Methods Are Legal On Go 1.27" entry only in its framing: that entry describes `Option[T].Map[R]`/`FlatMap[R]` against a three-state `Option` carrying `present`/`absent`/`failed`. The generic-methods finding stands; the three-state shape it was demonstrated on does not.

Rationale: an error stored inside an `Option` value is invisible to `errcheck`, to `go vet`, and to the compiler — nothing marks a caller who reads `Value()` and never notices the `failed` case. Go already has a place errors go, and tooling already knows to look there. Returning `(Option[T], error)` puts a lookup's failure in that position, so a caller who drops the `error` return is flagged exactly as they would be for dropping any other error. `Result[T]` keeps its `Err()` — it remains the type that carries a failure — and `Option[T]` narrows to the one question it should have only ever answered: is a value here.

This also brings the shape in line with prior art that keeps absence and failure apart by type rather than by convention: Rust's `Option<T>`/`Result<T, E>` are two separate types, and Vavr's `Option`/`Try` are likewise separate rather than one type wearing three hats. The three-state design was the outlier, not the destination.

Cost accepted: this is breaking for `anchor` and `echopoint`, whose repositories call `transactor.QueryOptional`/`QueryOptionalMap` and must move to handling the second `error` return at each call site; any caller of the removed `Failed`, `Err()`, or the two `ToOption` methods must also update. Worth noting honestly: the three-state design this replaces lasted one day, shipped in v0.11.0 and patched in v0.11.1 before this entry reverses it.

## 2026-08-21: Seq Replaces slicex

Delete `pkg/slicex`. `functional.Seq[T]` owns slice transformation.

`slicex` exported 8 helpers. A workspace-wide census found exactly one in use — `Map`, at 98 call sites across anchor and echopoint. The other seven were dead. Nested helper calls also read inside-out, the problem generic methods already solved for `Option` and `Result`.

`Seq[T]` is a defined slice type, so it and `[]T` assign to each other in both directions and nothing changes at an API boundary. `Map`, `Filter` and `FlatMap` keep the nil-preservation invariant `slicex` documented, which decides whether a handler emits JSON `null` or `[]`.

Measured, not assumed: `Seq` is identical to the `slicex` calls it replaces on time, bytes and allocations. Against a hand-written loop, `Map` and `FilterMap` are also identical, while a `Filter().Map()` chain costs +54% time and 2x memory for the intermediate slice, and `FindFirst` after a mapping chain is 48x slower than searching the source. Two ruleguard rules enforce the two idioms that avoid those costs.

A lazy `iter.Seq` pipeline was built and benchmarked as the alternative and rejected: 1.6x to 2.5x slower on every workload that materializes a result, plus 8 to 10 fixed closure allocations, buying a short-circuit path that `FindFirst` on the source already provides for free.

## 2026-10-15: Searches Page With Keyset Cursors

`PageBuilder` gains `Keyset(transactor.Keys(...))`, `search.Pagination` gains `Cursor` and `search.Result` gains `NextCursor`. Offset paging over a non-unique sort could skip or repeat rows, and shifted with every write before the current page. A keyset ordering always ends with a unique tiebreak and resumes after the last row's values, so a walk returns every row that exists throughout it exactly once. Details, alternatives and consequences are in [ADR 0001](adr/0001-keyset-cursor-pagination.md).

## 2026-10-16: Strict Search Sorts And A Search Conformance Kit

`transactor.StrictKeys` refuses a requested sort field it cannot map with `search.ErrUnknownSort`, which `StrictErrorHandler` answers as `400 INVALID_SORT`; `Keys` stays lenient for non-search orderings. `pkg/testkit/searchtest` checks a search through HTTP: cursor walks against fixture-defined expected order, refusals in every scope including empty ones, and forged framework cursors that must never produce a 5xx.

Rationale: reviewing the cursor-search rollout found 21 defects over 11 passes, and 16 fell in two classes this removes at the source: unknown sort fields silently falling back to the default (each service re-validated by hand and some missed it), and forged or extreme cursor values reaching Postgres as 500s (found one review pass at a time). A design review of the first proposal rejected an `Implicit` flag on `search.Sort` (it breaks positional literals and cannot identify every server-injected default) and a reference-page comparison (it cannot prove the requested sort was honoured), in favour of an opt-in strict constructor and explicit expected orders.

## 2026-10-24: OpenTelemetry Tracing In The Framework

`pkg/tracing` and `modules/tracing` build the tracer provider from the standard `OTEL_*` environment: OTLP/HTTP, batch processor, `ParentBased(TraceIDRatio)` (ratio from `OTEL_TRACES_SAMPLER_ARG`, default 1.0) and the W3C TraceContext + Baggage propagator. `modules/httpserver` traces inbound requests, `modules/postgres` wraps `lib/pq` with `otelsql`, and logs carry `trace_id`/`span_id`.

Rationale: one request must be followable from Traefik through echopoint to anchor, and log rows must link to that trace. Doing it once in the framework keeps span names, sampling and log keys identical across services.

Choices: without an endpoint the provider is a no-op but the propagator is still installed, so local runs need no collector and logs keep the edge's trace id. Span names use the chi route pattern, resolved after routing by `RouteMiddleware`, because raw paths carry ids. Database spans exist only under an existing span; otherwise pollers and lock heartbeats would emit a root trace per query. Trace fields come from a `log.Enricher` registered by `modules/logging` because `Bind` derives from the base logger: a field written only by `Contextualize` would be lost at the next `Bind`. Queue trace propagation is left to pgkit rather than duplicated in `modules/pgqueue`.

## 2026-10-30: Typed Shared State, Mutex And Pub/Sub On Redis

`pkg/redisx` gives typed `Value`, `Map`, `Set`, `Queue` and `Counter` structures, a `Mutex` and pub/sub `Topic`s from one `redisx.Client`, built with Go 1.27 generic methods (`rx.Value[Session]("session:1")`). `pkg/testkit/redistest` starts one real server per test binary on first use and gives each test its own key namespace.

Rationale: services need short critical sections that do not hold a database transaction open across remote calls, plus shared presence state and fan-out events. Each service had started to hand-roll these on go-redis. The code is written here from the documented Redis patterns (`SET NX PX`, compare-and-delete in Lua) instead of vendoring `bsm/redislock` (Apache-2.0) or `redsync` (BSD-3-Clause).

Choices: a lease expires, so a crashed holder cannot block the mutex forever, but a stalled holder can lose the lease while it runs. `Do` cancels the work when that happens, and every lease carries a fencing token for resources that can refuse stale writers. Absence comes back as `functional.Option`, never as an error. `Queue` and `Topic` are at most once, and durable work stays in `pgqueue`. Tests run against real Valkey and Redis instead of an in-memory fake, because the Lua scripts, `WATCH` and pub/sub timing are what needs proof.

## 2026-10-30: Mutex And Topic Hardened Against Server And Network Faults

A review of how a mature distributed-objects library and its issue history handle lock and pub/sub failures found seven defects in `pkg/redisx`, each now proven by a real-server test that failed before:

- go-redis sends a script again after a lost reply. The second run of the acquire script saw the caller's own lease and answered `ErrLocked`, and the lease stayed until it expired. The second run of the release script answered `ErrLeaseLost` after a clean release. Both scripts now give the same answer twice, and a `TryLock` that fails gives back a lease the server possibly took.
- A refresh against a server that stops answering blocked the keep-alive for the client read timeout and its retries, so `Do` cancelled the work seconds after the lease expired. The keep-alive now refreshes in its own goroutine and cancels at the expiry, counted from the send time of the last successful refresh.
- The keep-alive stopped when the caller context ended, while `fn` could still be running. It now refreshes until `fn` returns.
- The fence restarted at 1 after a server lost its data. It is now at least the server time in microseconds.
- A subscription that reconnected lost messages without telling anyone, and go-redis dropped messages after a handler was stuck for a minute. `OnError` now receives `ErrResubscribed`, and the backlog stays on the server until its buffer limit closes the connection, which is reported the same way.
- A panic in a topic handler killed the process. It now goes to `OnError`.
- go-redis 9.21 read a RESP3 push only once 36 bytes had arrived, so `Subscribe` on a short channel name waited for its deadline, or forever without one. The module now requires go-redis 9.22.0, which has the upstream fix.

Waiting `Lock` callers are now woken by a pub/sub message from the release, and poll at most once a second as a fallback, capped by the holder's remaining TTL. `redistest` gains `StartServer` (restart, pause, cut connections) and `FaultyClient` (lose the reply of a command).

Rejected, with reasons in the package README: fair ordering of waiters, waiting for replicas or a multi-server quorum (still timing-dependent; fencing is the answer), reentrant and read-write locks, one shared refresh loop, and exactly-once counters and queues under client retries.

## 2026-11-07: Check API Compatibility With Consumer Builds

Check public API compatibility by building each consumer against the framework change, as the [publishing runbook](runbooks/deployment.md) describes. Do not use `golang.org/x/exp/cmd/apidiff` on this module.

Rationale: apidiff recurses without end on Go 1.27 generic methods. `Option[T].Map[R any]` returns `Option[R]`, and each check of that result creates a new instantiation, so the recursion never meets a type it has already checked. One run on `pkg/functional` used 139 GB of memory and filled swap. The bug is in every apidiff version tested, up to `golang.org/x/exp` 47ce03d25ef0 (2026-11-06, "apidiff: support generic methods"). A three-line package with one generic method reproduces it.

The consumer build also fits the framework's current state. Versions are below v1.0, so a minor release may break the API, and every consumer is a Nanostack repository. A consumer build reports the breaks that matter, and an apidiff report would list changes no consumer uses.

Revisit when the framework reaches v1.0 or gains outside consumers, and when an apidiff release passes on `pkg/functional`.

## 2026-11-07: Go 1.27.3, Two Checks Off Until Go 1.27.4, And A Lint Smoke Test

`go.mod` pins `toolchain go1.27.3`, the newest Go 1.27 release; CI reads it through `go-version-file`. The lint standard stays on golangci-lint v2.15.0, which bundles staticcheck 2026.2.2 (v0.8.2), the first staticcheck that reads Go 1.27 export data.

Go 1.27.0 to 1.27.3 write export data that loses the order of generic methods ([golang/go#81188](https://github.com/golang/go/issues/81188)), so an analyzer can match a fact about an imported package to the wrong method, and staticcheck crashes in SA4023 and nilness. The bug is on the importer side: excluding `pkg/functional` would not protect the packages that import it. The compiler fix (CL 855582) is backported for Go 1.27.4, which is not released. Until then `lint/base.golangci.yml` disables staticcheck `SA4023` and govet `nilness`, each marked `TEMP(golang/go#81188)`. This is a temporary exception to the 2026-08-19 "The Lint Blocker Was A Stale Dependency" stance that no linter is disabled. The `pkg/ptr` nilness exclusion went with it, and the exclusion budget is 31.

`lint/smoke` holds a package that declares generic methods before ordinary ones, including the method shape of the go-tools#1746 reproducer, and a second package that calls both. `go test -tags lintsmoke ./lint/smoke/` lints the importer alone, so the first package comes from export data, then the whole fixture, then the importer again so facts come from the cache, then `pkg/functional` and `pkg/db/transactor`, which imports it, with the pinned golangci-lint. It fails on a non-zero exit, on a crash marker in the output, and on a run longer than 3 minutes, which it ends with `SIGQUIT` for a goroutine dump. The runs pass `--allow-parallel-runners`, so another golangci-lint holding the global lock neither fails a run nor uses up its time. The CI lint job runs it. It is a guard against crashes and hangs, not proof of the fix.

Measured on 2026-11-07 with golangci-lint v2.15.0 and Go 1.27.3: with SA4023 and nilness enabled, the fixture, the go-tools#1746 shape (cold and warm cache) and the whole repository lint without a crash. The only finding was the impossible `nil` check in the `pkg/ptr` test that the removed exclusion hid; that check is deleted. Because nothing here reproduces the bug, a smoke pass cannot show the fix. The checks stay off as the plan decided, because the upstream issue is open and the compiler fix is unreleased.

Revisit on Go 1.27.4: pin it, delete both `TEMP` entries, and lint the framework and every service with both checks on. That full lint is the re-enable test; then run the smoke test. The steps are in the [lint standard](../lint/README.md#generic-methods-and-the-smoke-test).

## 2026-11-07: Framework Direction Recorded As Proposed ADRs

Nine decisions from the framework plan are recorded as proposed ADRs, with their alternatives and consequences: [0013 generated wiring replaces fx](adr/0013-generated-wiring-replaces-fx.md), [0014 one repository with nested modules](adr/0014-one-repository-with-nested-modules.md), [0015 fork go-jet as the db module (multi-dialect)](adr/0015-fork-go-jet-as-the-db-module.md), [0016 generated request validation replaces kin-openapi at runtime](adr/0016-generated-request-validation.md), [0017 strict encoding/json/v2 decoding at the HTTP boundary](adr/0017-strict-json-v2-decoding.md), [0018 slog is the only logging API](adr/0018-slog-only-logging.md), [0019 net/http ServeMux replaces chi](adr/0019-servemux-replaces-chi.md), [0020 framework-owned db/migrate replaces golang-migrate](adr/0020-framework-owned-db-migrate.md) and [0021 harden pgkit/queue using River's design](adr/0021-harden-pgkit-queue.md).

They start at 0013 because the open [PR #59](https://github.com/nanostack-dev/nanostack-framework/pull/59) (feature architecture) holds 0002 to 0012, and the lint standard cites its 0012. ADR 0013 supersedes PR #59's ADR 0010 for how a service graph is assembled, and keeps its `exhaustruct` rule on `Deps` literals.

Each was judged on end-state quality only: if nothing existed yet, would the framework choose it? None of them changes code yet. Each becomes accepted when its phase starts, and the current-behaviour documents change with the code. The database library is decided by ADR 0015: the framework forks go-jet and keeps PostgreSQL, MySQL/MariaDB and SQLite.

## 2026-11-07: Error Catalogues From A Go Interface

Declare each module's errors as a Go interface annotated `//nano:errors`, one method per error, and generate typed constructors from it with `nano gen errors`. Every error states its stable code with `code=`. The framework plan's `errors.yaml` is not built. [ADR 0022](adr/0022-error-catalogue-from-go-interface.md) has the details.

Rationale: method parameters give each error typed fields, so a call site with a wrong field does not compile, and the generator type-checks its own output. An explicit code keeps the client contract (code and problem type URI) independent of the Go name. The OpenAPI `components/responses` fragment waits for the `http/problem` schema and is on the roadmap.

## 2026-11-07: HTTP Boundary Validates From The Spec And Fails Closed

Services decode request bodies with `http/nanohttp`, validate them with the validators `nano gen validate` writes from the OpenAPI document, and render every error with `http/problem` as RFC 9457 `application/problem+json`. The [HTTP boundary](http-boundary.md) is the contract.

Choices:

- The generated validators check the JSON text, not only the Go value: a JSON `null` body is rejected unless the schema accepts null, because json/v2 decodes `null` into a struct without an error.
- An integer accepts any JSON number with an integer value, such as `3.0`, as JSON Schema draft 6 and later (so OpenAPI 3.1) and kin-openapi define it. Typed json/v2 unmarshalers cover Go's integer types, and `nano gen validate` writes an `UnmarshalJSONFrom` method for each named integer type. An unmarshaler for every value (`any`) was rejected: json/v2 then allocates for every value in every body, which made decoding integers 7.5 times slower. The typed form costs no allocation, and only integers pay for it (about 2 times on an array of 100 000 integers).
- The document index that answers presence and null questions is a tree, linear in the body. An index keyed by JSON Pointer let a 200 KB body with a long key over a wide array allocate 788 MB.
- `problem.Write` logs through `log/slog` only. The base lint standard (`lint/base.golangci.yml`, depguard rule `deprecated`) bans `github.com/rs/zerolog` in every file. The legacy files in `pkg/` and `modules/` that still import it are listed one by one as exclusions in `.golangci.local.yml` until they migrate, so a new import fails lint anywhere else. A second, local zerolog rule was redundant and is removed.
- `http/problem` renders the `Described`, `Coded` and `Detailed` interfaces of `core/fault` and builds its type URIs from `fault.DefaultTypeBase`. There is one definition of the contract that generated catalogues implement.
- An unenforced schema feature fails generation unless its location is acknowledged with `-allow`, so a validation gap cannot ship unnoticed. The acknowledgement is now the `x-nano-unenforced` schema extension. Superseded by the 2026-11-08 "nano gen Runs Every Step In Order" entry below.
- `pattern` is translated from ECMA-262 to RE2 at generation time. An escape whose RE2 meaning differs from ECMA-262 is an error.
- `gen/oapi` generates the strict server on net/http's ServeMux with the template override, not chi.

## 2026-11-07: Generated Wiring Replaces Fx

`core/nano` and `nano gen wire` replace Fx, as the framework plan decided: a manifest is read at generation time and becomes plain Go, so graph errors fail generation instead of boot. The [wiring guide](generation.md#wiring-nano-gen-wire) describes the behaviour. This entry records the choices a review asked about, including where the code departs from the plan.

- **`HealthChecker.CheckHealth`, not `Check`.** The plan's name. A generic `Check(ctx) error` method made any type with it a readiness probe without the author meaning it.
- **An unused provider is an error, with no warning mode.** The plan said "warning, an error with `--strict`". Rejected: a warning in generated-code output is noise that nobody reads in CI, and an unused provider in the manifest's own package is dead code. Library modules are not affected: their unused providers are skipped.
- **`nano.Collect[T](fns...)` lists its members.** The plan's `nano.Collect[T]()` collected every provided implementation of `T`. Rejected: any new provider whose type happens to implement `http.Handler` would join the routes, and two contributors of one concrete type (echopoint's two providers of `log.Enricher`, pgkit's `*workflow.Definition` per feature) collide as duplicate providers. Listed members match Fx value groups and `nano.Bind`: membership is explicit, and members may share a type.
- **Generated provider errors carry no `file:line`.** The plan's example showed one. A position in generated code changes the file whenever a line moves above a provider, so `-check` would fail on unrelated edits. The diagnostic at generation time has the position; the runtime message names the provider function.
- **Every component registers through `App.Manage`, which registers a value once.** A provider that returns its dependency as an interface (`func AsHandler(s *Server) Handler`) ran `Server.Run` twice. Rejected alternative: refuse such providers at generation time. It also refuses legitimate builders (`func NewMux(h *UserHandler) http.Handler`) and leaves duplicate overrides unhandled. Identity is the same pointer or an equal comparable value, compared without reflection; values of a non-comparable type are never merged.
- **A Starter or Runner that something takes is built only when a built consumer needs it.** Before, every Starter and Runner was built, so a test that overrode `Store` still started the real `*PGStore` and its pool. Production is unchanged: without overrides every value in the graph is needed. A Starter or Runner that nothing takes is a root and always built.
- **Stop gives each Stopper at least `StopGrace` (one second).** With one shared `StopTimeout`, a Runner that ignored cancellation used the whole budget, and every later Stopper then started at once in the background, unordered, reported as late, and cut off by `os.Exit`. Stoppers now run one at a time in reverse order; the bound is `StopTimeout` plus `StopGrace` per Stopper called after it. Rejected: a separate drain timeout (one more knob that still lets one slow Stopper starve the rest).
- **A signal during start is a clean shutdown.** `Start` returns `ErrShutdownDuringStart` after stopping what started, and `Main` exits 0 unless a stop also failed or a Starter failed for its own reason.
- **One generator contract.** Package `gen` holds `File`, `ErrStale`, `Sync` and `Command`; `cmd/nano` routes `nano gen <name>` through a table. `gen/config`, `gen/errors` and `gen/validate` follow it too: `Main(args, stdout, stderr) int`, stale output wraps `gen.ErrStale`, and each is registered in `cmd/nano`. The separate `nano-gen-validate` binary is gone; `//go:generate` lines run `cmd/nano gen validate`.
- **`golang.org/x/tools` stays in the root module for now.** Superseded by the 2026-11-08 "Generators Move To A Nested gen Module" entry below. The plan makes `core`, `gen` and `cmd/nano` nested modules. Split now, they fall out of the root `go test ./...` and `golangci-lint run ./...` that CI runs, and they need `replace` directives until the first tags. No runtime package imports `x/tools` (`go list -deps ./core/nano` has none); consumers only see it in their module graph. Revisit with the repository consolidation (plan section 19), which adds per-module CI and tagging.
- **Liveness is `/livez`.** The plan's name, and the Kubernetes one that replaces the deprecated `/healthz`. `/infoz` is not built yet; the roadmap tracks it.
- **One generator per call.** `nano gen <name> -check` runs one generator. The plan's `nano gen [--check]` pipeline needs a per-project file with each generator's inputs, which does not exist yet; the roadmap tracks it. Superseded by the 2026-11-08 "nano gen Runs Every Step In Order" entry below.
- **Admin JSON uses `encoding/json/v2`** directly, and keeps it: the HTTP JSON helper (`http/nanohttp`) is in a higher layer, and `core` must not import `http`.

## 2026-11-07: One Secret Type, One Level Decision And One Field-Error Conversion

The framework branches were merged into one tree, and each concept now has one owner:

- **Secret.** `core/secret` holds the only secret type, `secret.Secret`, and the only redaction marker, `secret.Redacted` (`[REDACTED]`). `core/config` binds secret settings as `secret.Secret`, and `core/log` writes the same marker for secret-looking keys. `core/secret` also owns the rule for secret-looking names, `secret.IsKey` (moved from `core/log.IsSecretKey`), `secret.RedactURL`, which hides the userinfo password and every secret-named query parameter with the same marker, and `secret.RedactText`, which hides what looks like a credential in the rejected value a config issue shows. Generated config code calls `secret.RedactURL` directly: `core/config` has no second name for it. A `SecretProvider` returns a `secret.Secret`, so a store's value is never a plain string. `config.ParseText` names the expected type and drops the `UnmarshalText` error text, which often repeats the raw value. `url.URL.Redacted` is not used: it misses `?password=` and writes `xxxxx`. Config issues show no value for a URL setting that does not parse, because no rule finds a password in a malformed DSN reliably. `config.Secret` (marker `***`) and `log.Secret` (a `string`, so `%v` of an unexported field printed the value) are deleted. The value sits behind a pointer, so reflection shows an address. Rejected: a type alias in `core/config` or `core/log`, which keeps two names for one type.
- **Log levels.** `core/log` decides the level of an error in one place. `LevelFor` uses the error; `ErrorStatusTo` uses the status the caller already answered. `http/problem.Write` logs through `ErrorStatusTo`, so an error that picks its own level keeps it in problem responses too. A `ProblemStatus()` outside 400 to 599, or a `Described` with an empty `ProblemType()`, counts as no status, as the problem writer renders it as a 500. `http/problem` and `core/log` apply this through one rule, `fault.Valid` (with `fault.IsErrorStatus` for the legacy `pkg/fault` errors, which have no type).
- **Log keys.** `core/nano` logs lifecycle errors under `log.KeyError` and the component under `log.KeyComponent`. It no longer adds an `app` attribute: the process identity is `service`, which `core/log` adds from `Config.Service`, and a second key would duplicate it. The cost: with a handler that is not `core/log`'s, or without `Config.Service`, lifecycle lines carry no identity. Such a service passes `logger.With(log.KeyService, name)` to `App.SetLogger`. Rejected: adding `service` in `core/nano` only when the handler does not, which needs a probe of the handler and still duplicates the key behind a wrapping handler.
- **One Error line per failure.** A lifecycle failure is logged at Error once, where it happens, with its component. The summaries (`nano: stopped`, `nano: exiting`) are Info and carry a count, not the joined error. A Starter that fails for its own reason while a shutdown is requested is a failure: it is logged at Error and `Main` exits 1. Only a Starter that returns the cancellation counts as a clean shutdown.
- **Field errors.** `validate.Error` is the domain shape and `problem.FieldError` the wire shape. Both name the human text `Detail`, the RFC 9457 member, and `http/problem` converts one to the other in one function.
- **Names.** `core/config` reports `config.Issue` values in one `*config.Error`. `Problem` now means only an RFC 9457 object, and `Errors` only the `core/validate` list.
- **ADR number.** The error catalogue ADR is 0022, not 0002, because PR #59 holds 0002 to 0012.
- **Layers are enforced.** Plan section 06 says a package imports only its own layer and the layers below: `core`, then `http`, then `gen` with `cmd`. The `internal/layers` test checks the import graph, test files included, in every `go test ./...`. Depguard rules in `.golangci.local.yml`, not in `lint/base.golangci.yml`, report the same rule in editors: the base ships to every service, whose `core/` directories are their own.
- **Two Error lines for a failed request.** `request failed` holds the error; the `http request` summary is the access record and stays Error on 5xx, so a 5xx that no handler logged is still visible. Alerts count `msg="request failed"`, or the summary's `status`, never every Error line ([core/log](../core/log/README.md)). Rejected: an Info summary after an error line, which hides the 5xx from a query on the summary's level and couples the middleware to every error helper.
- **A cancelled request is a client-closed request.** `problem.Write` answers 499 or 504, and logs at Warn, when the request context is done and the error has no status of its own, even if the error does not wrap `context.Canceled`. This matches the request summary.
- **Generators share one import namer and one error style.** `gen/internal/importset` names the imports of every generated file (`config`, `errors`, `validate`, `wire`); an import never takes a name the package declares, a generated local or a predeclared identifier. `gen.Sync` returns file-system errors as they come and a `*gen.StaleError` that lists the stale files; each `Main` adds the `nano gen <name>: ` prefix once. `nano gen wire` writes `wire.gen.go`, so every generated file matches `*.gen.go`. Rejected: keeping google/wire's `wire_gen.go`, which makes agents and lint exclusions learn two patterns.

## 2026-11-08: Beta Readiness Review Fixes

A review of `feat/framework-beta` found these gaps. Each decision says what changed and what was rejected.

- **Design B3 slips past the first beta.** Each nano module is its own implementation, and the deprecated fx module keeps its own code. They differ: `redis/nanocache.ErrCacheKeyNotFound` and `modules/cache.ErrCacheKeyNotFound` are different values, `pg/nanopg` quotes every DSN value, `pg/nanomigrate` accepts an empty migration directory, and `redis/nanocache.EvictPattern` uses `SCAN` where `modules/cache` uses `KEYS`. `CHANGELOG.md` lists them as a known risk and [docs/modules.md](modules.md) has the full table. Making each fx module a wrapper of its nano module changes the behaviour of the fx modules that echopoint and anchor run today (the DSN quoting, the empty directory and `SCAN` are such changes), so it needs the golden HTTP suite of each service first. The wrappers come in beta.2. Rejected: a partial wrapper now (for example only the cache sentinel alias), which leaves two half-shared implementations.
- **The integration layer is enforced.** `internal/layers` ranks `pg`, `redis` and `otel` as layer 2, next to `http`, and the test lists the integration test files too. A layer-2 package may import only the `pkg` primitives in `layers.AllowedLegacy`, and a test checks that no runtime layer, and no allowed primitive, depends on Fx or zerolog. Depguard reports the same rules, and bans Fx in every layered tree. `redis/nanocache` tests now use `redis/internal/redistest`: `pkg/testkit/redistest` depends on `pkg/redisx`, which carries zerolog. Rejected: allow-listing `pkg/testkit/redistest`, which brings zerolog into a layered test binary.
- **A missing parameter keeps its binding error.** `problem.MissingParameter(name, cause)` is the 400 for a required parameter that is absent, and `problem.InvalidParameter` is always "is invalid". The generated `paramProblem` passes the binding error as the cause in every case, so a legacy renderer can write anchor's text/plain message and tell a missing query parameter from a missing header. Rejected: selecting the code by a nil cause, which drops the binding error.
- **`nanohttp.NewRouter` keeps every ServeMux redirect.** A request with an unclean path goes to the mux for every method. The first version answered `POST //health` with 405 and `//nothing` with 404, a third behaviour that matched neither chi nor ServeMux. Rejected: answering every unclean path with `NotFound` for chi parity, which also drops the redirect of `GET //health` that ServeMux users rely on.
- **`pg/nanomigrate` gives its connection back.** `Migrate` takes one connection from the pool, hands it to golang-migrate with `postgres.WithConnection`, and closes it at the end; `modules/migrations` does the same. `postgres.WithInstance` kept one connection checked out for the life of the process. Rejected: `Migrate.Close`, which also closes the application's pool.
- **A stop during migrations reports what happened.** `ErrStopped` is returned only when no migration failed and one is still pending. A failure during the stop is returned as the failure, without the cancellation, so `nano` does not take it for a clean shutdown.
- **The fx bridge gives a late Stop the stop grace.** When a Run misses the OnStop deadline, Stop gets `nano.StopGrace` from its call, as in `nano.App`, instead of an expired context.
- **`internal/nanoexample` wires every nano module.** CI runs `nano gen wire -check` and `nano gen config -check` on it (now one `nano gen -check`, see the 2026-11-08 pipeline entry), and its test binds the nested `nano.AppConfig` and `log.Settings`. Rejected: adding those fields to the `gen/config` golden fixture, whose inlined `log_level` setting would collide with the fixed `LOG_LEVEL` name of `log.Settings`.

## 2026-11-08: Beta Readiness Second Review Fixes

A second review of `feat/framework-beta` found these gaps. Each decision says what changed and what was rejected.

- **The legacy error envelope ships in beta.1 (design C3).** The first beta text let C3 slip: a service on the nano HTTP stack had only `modules/httpserver.StrictErrorHandler`, which wrote the `http/problem` `Error()` text (internal code, title and decoder text) as the message of every 400, turned 415 into 400, answered a `core/fault.Described` with 500, and logged its 5xx cause line through zerolog without `request_id`. `pkg/httputil/errorenvelope` now does what C3 decided. `Resolve` (handler errors) and `ResolveRequest` (request-hook errors) decide the status and the body: `pkg/fault` errors byte-identical to `fault.WriteJSON`, the search cursor errors, boundary problems as one detail per field error (`<location>: <detail>`, metadata `field` and `rule`, 413 and 415 kept), a `Described` with its status and its code in upper case, 499 with no body for an abandoned request, and SSE frames. `Writer` writes them for the strict hooks and the router, and logs one `request failed` line through `core/log` with the request context, so the line has `request_id`. `StrictErrorHandler` uses the same resolution and keeps its zerolog lines. Deviations from the C3 text: `Response` has no `Level`, because the two writers have different level policies (`core/log.LevelFor` against the v0.22.0 zerolog levels); a request-hook error that is not a boundary problem keeps its text as a 400 message, as v0.22.0 does for the errors of the chi strict server; `Writer.Handler(*fault.Error)` gives a router a service's own answer, such as echopoint's 400 for an unknown path. Rejected: document that a service must stay on the chi strict server until beta.2, which blocks the HTTP migration of both services.
- **`http/nanoapisec` exists, and `modules/apisec` wraps it.** `pkg/apisec` is on the integration allow-list, so the reason given for its absence was false. `modules/apisec` calls `nanoapisec.NewResolver` and keeps its fx signature, so apisec has one implementation. This is not the partial wrapper that the B3 slip rejects: the whole module is the wrapper, and its behaviour does not change.
- **`pkg/functional` is the leaf layer.** Plan section 06 puts `functional` below `core`. `internal/layers.Leaves` lists it: it is not legacy, and every layer may import it, `core`, `gen` and `cmd` included. Depguard allows it in the core and tooling rule. Rejected: keep it on the integration allow-list, which keeps `core` from using `Option` and `Result`.
- **Two more router differences from chi are accepted.** `Allow` on a 405 lists every method of the path (`GET, PATCH`), where chi v5.3.1 lists one. A percent-encoded unreserved character (`/he%61lth`) matches the decoded route, where chi answers 404. [docs/http-boundary.md](http-boundary.md) lists them, and each service PR lists them too.
- **`pg/nanomigrate` reads the stop only after its watcher ends.** golang-migrate reads `GracefulStop` at once and `Up` returns nil, so a check made before the watcher finished could report a stopped run as complete and skip `ErrStopped`.

## 2026-11-08: Development Form and Release Branches

Design D2 and D3 put the root `go.mod` in its release form (tagged nested versions, no `replace`) on the beta branch itself. That branch could not pass CI: `go.sum` cannot hold the hashes of versions that are not tagged, and the tags must point to commits on `main`, which the branch reaches only after it passes CI. Every later pull request that changes `db` and the root together has the same loop.

- **`main` keeps the development form.** A released module requires each other module of this repository at the placeholder `v0.0.0`, with a `replace` to its relative directory. The release check fails a pull request where one goes without the other, or where a `replace` points anywhere else.
- **Release commits live on a `release/vX.Y.Z` branch.** The tags go leaves first: `pkg/functional` on the `main` commit, then one commit for `db/go.mod` and one for the root `go.mod`, each tagged. The release form at a tag is unchanged: no `replace`, no `v0.0.0`, no pseudo-version, and each required version is a tag in the history. The tag check and the scratch consumer build of `release.yml` test exactly what consumers get.
- **Why this is the end state, not a workaround.** CI on `main` tests the code of one commit together, which is what a release tags. The design's objection (plan section 19: "CI tests the versions in go.mod") holds for third-party modules, which keep their versions; for this repository's own modules, the version in the development form is the commit itself. Rejected: **a permanent `replace` with real versions** (OpenTelemetry style), because `go install github.com/nanostack-dev/nanostack-framework/cmd/nano@vX.Y.Z` refuses a `go.mod` that has a `replace`. Rejected: **pseudo-versions of pushed commits on `main`**, which need a push and a `go.mod` commit for every cross-module change.

## 2026-11-08: Beta Audit Fixes

An audit of `feat/framework-beta` before v0.23.0-beta.1 found security, concurrency, API and performance defects. Each bullet says what changed and the rejected alternative. The [CHANGELOG](../CHANGELOG.md) lists the wire changes in its Security section.

- **Validation errors stay a slice, capped at `MaxErrors` + 1 (sec-http-1).** `validate.Errors` keeps its slice type and stops growing past 101 entries; `Full` tells the generated loops to stop, `Listed` gives the first 100 and `Truncated` the overflow; the generated code skips items past `maxItems` and members past `maxProperties`, and fills one accumulator through `validateInto`. Rejected: a struct with a total count, which breaks every `len(errs) > 0` caller, the committed benchmarks included.
- **The security resolver matches as the router does (sec-http-2).** `apisec.NewResolver` indexes the operations in a private `http.ServeMux` (the matcher of `nanohttp.NewRouter`), or with `MatchChi` in a private chi mux with chi's raw-path rule; `modules/apisec` uses `MatchChi` because echopoint and anchor route with chi. Rejected: keeping gorilla/mux, which disagrees with both routers on `servers`, encoded paths and HEAD; ServeMux only, which under-protects a chi service and cannot load echopoint's spec.
- **`nanohttp.NewRouter` owns a catch-all route, and a mux is wrapped once (bench-router-miss, bench-router-hit).** Every request goes straight to `mux.ServeHTTP`; a miss lands on the catch-all and answers 404 or 405. Rejected: replaying a miss through a header-capturing writer, which keeps two lookups on every hit.
- **`DrainDelay` exists and defaults to 0 (concurrency-readiness-no-lead-time).** Stop turns readiness unavailable, then keeps the Runners serving for `DrainDelay`, inside `StopTimeout`. Rejected: a non-zero default, which slows every stop, tests and local runs included, by a delay that depends on the load balancer. Start fails when `DrainDelay` leaves the Runners less than `StopGrace` of `StopTimeout`, and the drain waits on a timer that a Stop context cancelled during it ends. Rejected: clamping the delay, which cuts the drain short in silence while the load balancer still routes to the instance.
- **A signal during the build exits 0 (api-main-build-signal-exit).** When the build failed only from the cancellation, `Main` logs the shutdown and exits `ExitOK`; `nanomigrate.ErrStopped` keeps the cause in one chain so a sibling failure still exits 1. Rejected: exit 1 for any build error, which reports every rollback during a migration as a failure.
- **An interrupted Starter is waited for (concurrency-nano-abandoned-starter-never-stopped).** The App waits for it until the start deadline and stops it when it succeeds late. Rejected: abandoning it, which stops its dependencies under it and leaks what it acquires.
- **`/readyz` shows `ok` or `fail` (sec-core-02).** The error text goes to the log once per change. Rejected: redacting the error text, which cannot recognise every host name or DSN fragment. The legacy `/health` body keeps its text until the gate flows that read it are updated (AGENTS.md, cross-repo rollouts).
- **Redaction errs towards hiding (sec-core-01, sec-core-04).** `RedactURL` redacts the whole text of a URL that parsed into a suspicious shape; `IsKey` adds suffixes and judges the last bracketed segment, through a table indexed by the key's last byte. Rejected: locating the password inside a misparsed URL, which cannot be done reliably.
- **Spec text is escaped in generated comments, and the spec model is internal (sec-supply-01, sec-supply-02, api-gen-validate-exported-model).** `commentText` escapes control characters and line separators; YAML booleans decode as YAML does; `gen/validate/internal/oas` holds the spec model. Rejected: refusing specs with such text, which fails generation on text that is harmless once escaped; keeping the model exported, an API with no consumer to keep stable.
- **Text bodies are read under the body limit (sec-http-3).** Rejected: leaving `text/plain` unbounded because the generated JSON path was bounded.
- **Dependencies are bumped and govulncheck gates CI and releases (sec-supply-03).** Rejected: a scheduled scan only, which lets a release ship between two scans.
- **`nanolock` takes the `nanopg` pool through `pglock.UnsafeNew` and checks the lease itself (api-nanolock-otelsql).** Rejected: unwrapping the otelsql driver to satisfy `pglock.New`, or patching pglock.
- **A drain that reaches `shutdown_timeout` succeeds (concurrency-nanoserver-stream-drain-fails-shutdown).** `nanoserver` and `nanopprof` close the remaining connections, log one Warn line (with the count for `nanoserver`) and return nil; `nanoserver.Draining(ctx)` lets a stream end first. Rejected: returning the deadline error, which makes a planned stop exit 1 whenever an SSE client is connected; per-handler shutdown hooks, a registration API for what one channel does.
- **`nanofx.Context` provides the context on fx (api-nanofx-missing-context).** Rejected: `fx.Supply(ctx)`, which provides the dynamic type, not `context.Context`; constructors without a context, which cannot stop a dial on SIGTERM.
- **The `yaml:` tags stay, documented as legacy fx keys (api-yaml-tags-dead).** Rejected: deleting them, which breaks a service still on the fx loader; making `core/config` read them, which gives a setting two names and an ambiguous precedence.
- **`nanotrace` splits the install from the flush (concurrency-nanotrace-shutdown-gated-on-start).** `Installer` is the Starter; `Provider` is a start-less Stopper, so a failed build still flushes. Rejected: a cleanup `func()`, which ignores the stop deadline; installing in the constructor, a global side effect of a build that may fail; taking `*nano.App`, which fx services cannot provide.
- **The bound OTLP endpoint reaches the exporter (api-nanotrace-endpoint-yaml).** `traces_endpoint` as given, `endpoint` with `/v1/traces` joined, an invalid URL fails the build. Rejected: passing both as given, which sends a base endpoint to the wrong path; keeping the env-only caveat, which accepts a YAML value and ignores it.
- **`nanomigrate` takes the migration lock itself, in a loop a stop interrupts (concurrency-nanomigrate-sigterm-ignored-on-lock-wait).** Session advisory locks are re-entrant, so golang-migrate's own lock then returns at once; a connection whose release is not confirmed is discarded. Rejected: forking golang-migrate's postgres driver for one query; `lock_timeout`, which fails the wait instead of stopping it. `modules/migrations` keeps the old wait: its API has no context, and it is frozen.
- **Cache key parts have one encoding (sec-db-001).** `:` and `\` are escaped in parts, glob characters in patterns, and `EvictPattern` unlinks page by page; the prefix is used as given. Rejected: rejecting such parts, which needs an error path that `Key` does not have; encoding the prefix, which changes every key for no gain; fixing `modules/cache`, which is frozen and has no caller of `EvictPrefix`. `nanocache.New` returns `ErrInvalidPrefix` for an empty prefix, a prefix with a glob character (`* ? [ ] \`) or a trailing `:`, and its doc states that prefixes must not nest (`a` and `a:b` share keys); a hierarchical prefix such as `billing:product` stays valid.
- **Module configs follow one zero-value rule (api-zero-value-config).** Constructors default zero durations and sizes; `DefaultConfig()` is written by hand and a test (`configtest.AssertDefaults`) keeps it equal to the `default:` tags. Rejected: generating `DefaultConfig`, a generator mode for three to six fields per module; reading the tags by reflection at run time.
- **Generated field names do not depend on the graph order (api-wire-field-names).** Every value whose type name collides is qualified by its package, with Go initialisms; a slice of a name ending in `s` gets `List`. Rejected: the first value keeping the bare name, so that a reordered graph renames another field; qualifying every field, which is noise when nothing collides.
- **Legacy packages carry `Deprecated:` paragraphs (api-deprecated-markers).** `modules/nanofx` does not, because every migrating service imports it. The framework's own legacy-to-legacy imports are excluded per file, which raises the exclusion budget from 73 to 77; the searchtest integration test moved to `errorenvelope.Writer` instead of taking an exclusion. Rejected: disabling SA1019 globally, which also hides the deprecations in `db`; `//nolint` lines, which bypass the reviewed budget.
- **An error frame after a stream started does not write a second status (concurrency-errorenvelope-stream-frame-edge-cases).** `core/log.ResponseStatus` reads what `HTTPMiddleware` recorded; the `request failed` line reports the status the client received. The exact `text/event-stream` match stays for wire parity. Rejected: inferring "started" from the Content-Type, which drops the error status of a handler that set the header and wrote nothing.
- **The Option scan decides Scanner once per field (bench-scan-option).** Rejected for now: writing into the private fields of `functional.Option` through `unsafe`, which ties `db/qrm` to the layout of another module for the remaining 9% and 0.75 allocation per non-NULL value; an exported scan hook on `functional.Option` is the better end state and a separate decision.
- **`bench-validate-allocs` is not changed.** The proposed fix does not remove the cost, because `document.lookup` walks the pointer on every presence query, and the path did not regress. The empty-container index nodes of sec-http-1 took the cheap part.

## 2026-11-08: Generators Move To A Nested gen Module

The [nano gen pipeline design](design/nano-gen-pipeline.md) (implementation plan, PR 1; roadmap, deferred generator output 3) links oapi-codegen and testcontainers into the generators. In the root module, every runtime consumer would resolve them, as it resolves `golang.org/x/tools` today. This supersedes the 2026-11-07 bullet "`golang.org/x/tools` stays in the root module for now".

- **`gen/...` and the CLI are the module `github.com/nanostack-dev/nanostack-framework/gen`.** `cmd/nano` moves to `gen/cmd/nano`; the `gen/...` import paths do not change. The root `go.mod` no longer requires `golang.org/x/tools` or `oapi-codegen/nullable`. `internal/layers` checks it: the tooling layer is listed from `gen/`, and a test fails when the root module lists a `gen` or `cmd` package, requires `gen` or `x/tools`, or builds a package with `x/tools`.
- **`gen` requires the root; the root never requires `gen`.** So the tag order is `pkg/functional`, `db`, the root, then `gen` (RELEASING.md), and `release.yml` checks and builds a consumer of `gen/vX.Y.Z` tags. `gen/go.mod` keeps the development form: `v0.0.0` and a `replace` for the root, and the root's two `replace` lines repeated, because a dependency's `replace` does not apply in the module that requires it.
- **Services run `nano` through a `tool` directive.** `go get -tool .../gen/cmd/nano@<tag>`, then `go tool nano gen <name>`. The tool's version is in `go.mod` and `go.sum`, and `go run .../cmd/nano` without a version, which ran the generators of whatever root version the service required, goes away.
- **The framework's root module runs `nano` with `go -C`.** Its `//go:generate` lines and CI run `go -C <path to gen> run ./cmd/nano` with `-dir` pointing back. Rejected: **a `tool` directive in the root `go.mod`**, which requires `gen` and brings `x/tools` back into the root graph, the thing this change removes. Rejected: **a committed `go.work`**, which RELEASING.md rule 4 forbids, because CI must test each module from its own `go.mod`.
- **`cmd/nano` moves into the module, not into a second tools module.** Rejected: **leaving `cmd/nano` in the root**, which keeps every generator import in the root graph. Rejected: **a separate `tools` module for the CLI only**, a fifth module to tag for one `main` package that imports nothing but `gen`. ADR 0014's `tools/` module pins third-party tools; `nano` is the framework's own generator.
- **`gen` has its own lint configuration and CI job.** `gen/.golangci.local.yml` carries the tooling layer's depguard rules (no legacy tree, no Fx), which leave the root configuration. Lint runs in a workspace of the root and `gen`, as for `pkg/functional`, because the ruleguard rules import a package only the root requires. CI also runs `go mod tidy -diff` in the root and in `gen`.
- **Golden headers change once.** `nano gen wire` writes the manifest path relative to its module, so the `gen/wire/testdata` goldens read `wire/testdata/...` instead of `gen/wire/testdata/...`. Service outputs do not change.

## 2026-11-08: Generator Errors Name Their Fix, And Config Loads Under nanogen

The [nano gen pipeline design](design/nano-gen-pipeline.md) (implementation plan, PR 2; decisions P4, P5 and P7) changes the generator contract before the pipeline uses it. `config`, `errors`, `validate` and `wire` follow the new contract.

- **Every error is a `*gen.Error{File, Line, Msg, Fix}`.** Several are joined with `errors.Join`. `gen.Errorf` takes the fix before the message, so a call cannot leave it out, and each generator's error table test fails on an error without one. `gen.Report` prints `file:line: msg`, or `nano gen <name>: msg` without a file, then `  fix: <fix>`. An agent reads the position, the problem and the change in one place. `wire.Error` and `wire.Diagnostic` go away. Rejected: **a fix appended to the message after a `;`**, the old form, which a caller cannot tell from the message; **a test of the message text only**, which lets a new error path ship without a fix.
- **Exit 3 means the environment failed.** An error whose `Err` field holds `gen.ErrEnvironment` (`Unwrap` returns it) exits 3: `packages.Load` cannot run the go command, or a file cannot be read or written. A source error and stale output still exit 1, and bad usage 2. The pipeline will add Docker and toolchain checks to the same code. Rejected: **exit 1 for both**, which tells an agent to change correct code when the machine lacks a tool.
- **`Options.Overlay` in `config`, `errors`, `validate` and `wire`.** It replaces files in every `go/packages` load, keyed by absolute path; a relative key is an error. The overlay never replaces a generator's own output: `config` loads a stub, `errors` and `validate` an empty file, and `wire` drops `wire.gen.go` under `-tags=nanogen`. `validate` now always stubs its output, not only when the file exists. The spec stays an input that `validate` reads from disk. A `-check` run of the pipeline overlays the fresh outputs of earlier steps. Rejected: **a temporary copy of the module**, which the overlay replaces at no cost.
- **`nano gen config` loads with `-tags=nanogen`.** That drops `wire.gen.go` and the files that call `Build` (all `//go:build !nanogen`) and keeps the manifest, which `nano gen wire` already loads the same way. A renamed `Config` field no longer deadlocks config and wire; the `gen/config/testdata/deadlock` fixture proves it. `Config` must sit in a file without a build tag, as it does in both services. Rejected: **stubs that hold only a package clause**, because `run.go` calls `Build`, which `wire.gen.go` declares; **deleting outputs to recover**, which loses tracked files.
- **`validate` reports each unenforced feature as its own error.** Each wraps `genvalidate.ErrUnenforced`, names the spec and the JSON Pointer, and gives the `-allow` fix (now the `x-nano-unenforced` fix, at the schema's line). Spec errors from the YAML parser carry their line; errors about a schema location carry line 0 until the api step loads the spec with origins (design PR 5).

## 2026-11-08: nano gen Runs Every Step In Order

The [nano gen pipeline design](design/nano-gen-pipeline.md) (implementation plan, PR 3; decisions P1, P2, P3, P5, P6, P7, P11 and P12) adds the pipeline core. [ADR 0023](adr/0023-nano-gen-pipeline.md) records it. Roadmap item 2 of the deferred generator outputs is done for validate, errors, config and wire.

- **`nano gen` with no name runs validate, errors, config and wire in that order.** Generate writes after each step. `-check` writes nothing and gives later loads a `go/packages` overlay of the fresh outputs. An existing output of a later step is replaced by a stub with panicking bodies in every load, in both modes. Rejected: **parallel steps**, because wire type-checks every earlier output; **everything in memory until the end**, because an overlay cannot add a whole directory.
- **The project declaration types are in the root module, at `core/nano/project`, not in `gen/project`.** This refines design P2. The root module never requires `gen`, and its example service, `internal/nanoexample`, declares a project. `go mod tidy` reads every build tag except `ignore`, so a tag cannot hide the import of `gen`, and a declaration under `ignore` is type-checked by nothing. The types are plain data, like `nano.Manifest`, and import nothing. The reader is `gen/pipeline`: it type-checks `nanogen/project.go`, then reads the literal from the syntax tree, so a value must be a literal or a constant. Rejected: **a nested module for `internal/nanoexample`**, a module that the release checks do not know, and a declaration read from one module for the generation of another.
- **`-dir` names the directory a run covers, by default the module root.** The framework runs the pipeline on `internal/nanoexample`, a package of the root module. A run from the root module would walk `http/nanohttp/internal/example`, whose oapi-codegen line has no replacement until the api step.
- **Discovery parses; it never type-checks.** A `//nano:errors` declaration adds an errors unit; a `//go:build nanogen` file that calls `nano.Manifest` adds a config and a wire unit. wire writes into the manifest package. `testdata`, `vendor`, `node_modules`, hidden and `_` directories and nested modules are skipped.
- **Orphans come from the `Code generated by nano gen <step>` header.** A file with it, for a step that ran, that the run did not produce, is deleted by a run and fails `-check`. The scan walks the whole directory, so a deleted catalogue's outputs are found although their package is no longer discovered. validate has run when the declaration has no `API`, so its output is an orphan; it has not run in a module without `nanogen/project.go`, which keeps a `validate.gen.go` during the move to the pipeline. It does not run when a Go file fails to parse. Rejected: **an outputs manifest or a lock file**, which churns and restates the header.
- **A failed step skips only the steps that need it.** config needs validate and errors; wire needs config. validate and config read `nanogen/project.go`: a declaration with an error skips them, and wire. A broken declaration otherwise wrote the `Env` file to its default path and deleted the declared one as an orphan (found in review). A validation gap fails the run but skips nothing. Rejected: **stop at the first failure**, because an agent needs every fix from one run.
- **`x-nano-unenforced: "<reason>"` replaces `-allow`.** It sits on the schema it accepts and covers the schemas under it. An empty reason is an error, and so is an extension that accepts no gap. `validate` errors now carry the schema's line. Rejected: **a waiver list in the project declaration**, far from the schema.
- **Two guards.** An unknown `//nano:` directive and a `//go:generate` line that runs `nano gen` are errors at their line. The check for a `//go:generate` line that runs oapi-codegen (P12) waits for the api step (design PR 4): until then that line is the only way to regenerate the server, and a guard must not reject what the pipeline cannot replace yet.
- **Output for agents.** A clean run prints nothing. `-json` prints one event per line, `{step, level, file, line, msg, fix}`, with `diff`, `inputs` and `outputs` when they apply. Problems found before any step runs have the step `plan`.
- **Alexis's answers to the design's open questions** (delegated: take each recommendation) do not touch this PR. They concern mappers, anchor's `ProductUserStatus`, the JSON decode policy and a lint rule, and the PRs on those topics record them.
