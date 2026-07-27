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
