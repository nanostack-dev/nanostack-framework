# Changelog

One section per module and version, newest first. Each section uses Added, Changed, Deprecated, Removed and Fixed. [RELEASING.md](RELEASING.md) gives the release steps and the upgrade notes.

## v0.23.0-beta.1 (root)

### Added
- `core/` (secret, log, config, fault, validate, nano), `http/` (problem, nanohttp), `gen/` and `cmd/nano`.
- Nano modules: `core/logging`, `otel/nanotrace`, `pg/nanopg`, `pg/nanomigrate`, `pg/nanolock`, `redis/nanocache`, `http/nanoserver`, `http/nanoapisec`, `http/nanopprof`; the `modules/nanofx` bridge. `modules/apisec` is a thin wrapper of `http/nanoapisec`. [docs/modules.md](docs/modules.md) lists them.
- `core/log`: `Config.Wire` (`WireLegacy`) and `ParseWire`, `Settings`, `ContextAttrs` (attributes read from the context of a record), `HTTPOptions.LegacySummary`, exported summary keys.
- `http/problem`: `NotFound`, `MethodNotAllowed`, and `MissingParameter` (a required parameter is absent; the generated parameter binding keeps its error as the cause).
- `http/nanohttp`: `NewRouter` (ServeMux with chi-compatible HEAD, 404 and 405 answers). The generated `NewHTTPHandler` takes `HTTPHandlerOptions.RequestErrorHandlerFunc` and `ResponseErrorHandlerFunc`; they default to `nanohttp.RequestError` and `nanohttp.ResponseError`.
- `http/nanoserver`: `Readiness` (`NewReadiness`), the App's readiness as a `pkg/health` check for `/health`.
- `nano.AppConfig`: `config` tags (`start_timeout`, `stop_timeout`, `health_timeout`) for `nano gen config`.
- `pkg/httputil/errorenvelope`: the writer of the legacy error envelope (`{"errors":[{code,message,metadata}]}`, `application/json`) for the strict hooks and the router, on the oapi-codegen chi strict server and on the nano HTTP stack. `Writer` logs the `request failed` line through `core/log` with the request scope (`request_id`). A service that moves its logging to `core/log` replaces `StrictErrorHandler` with `errorenvelope.New(...).RequestError` and `ResponseError`. Boundary errors render as `<location>: <detail>` with metadata `field` and `rule`, not as kin-openapi text: update the gate assertions on that text first ([docs/http-boundary.md](docs/http-boundary.md)).
- `internal/layers.Leaves`: `pkg/functional` is the leaf layer, and `core/`, `gen/` and `cmd/` may import it.
- `.github/workflows/release.yml`: the go.mod release check on every pull request, and a scratch consumer build for each release tag.

### Changed (breaking)
- `pkg/db/transactor` and `pkg/jetx` take Nanostack DB types (`github.com/nanostack-dev/nanostack-framework/db/...`) instead of go-jet. Rewrite go-jet imports and regenerate models.
- `pkg/functional` is its own module (same import path). `go mod tidy` adds it.
- The root module requires `db` and `pkg/functional` at tagged versions and no longer requires `github.com/go-jet/jet/v2`.

### Changed
- `modules/httpserver.StrictErrorHandler` uses the resolution of `errorenvelope`. Its answers for v0.22.0 inputs do not change, with three additions: a `core/fault.Described` error (a generated catalogue error) gets its own status and its code in upper case instead of 500 `UNEXPECTED_ERROR`; an `http/problem` request error of the nano HTTP stack gets one detail per field error (`<location>: <detail>`) and keeps 413 and 415, instead of the problem's `Error()` text in a 400; an `AdaptError` that returns `(nil, true)` counts as no match (500 `UNEXPECTED_ERROR`, logged as unhandled).
- `modules/apisec` is a thin wrapper of `http/nanoapisec`. Its fx signature does not change.

### Fixed
- `modules/migrations` (and the new `pg/nanomigrate`) give their connection back to the `*sql.DB` pool after a run. Before, each run kept one connection checked out for the life of the process.
- `modules/migrations`: `validate: false` turns the down-file warnings off also while migrations are enabled.

### Deprecated
- Every fx module in `modules/` (including `modules/httpserver.StrictErrorHandler`), `app/`, `pkg/fxlog` and `pkg/httputil/requestlog`. Each package doc says so. They keep working in the beta.

### Known risks: two implementations of each capability
Each nano module is a separate implementation, not a wrapper of its fx module (design B3 slips past this beta; see the [decision log](docs/decision-log.md)). Do not mix the values of the two packages in one service. The behaviour differences are in [docs/modules.md](docs/modules.md#differences-from-the-fx-modules). The ones that can surprise a service:
- `redis/nanocache.ErrCacheKeyNotFound` and `modules/cache.ErrCacheKeyNotFound` are different values. `errors.Is` across the two packages is false.
- `pg/nanopg` quotes every DSN value; `modules/postgres` does not.
- `pg/nanomigrate` succeeds on an empty migration directory; `modules/migrations` fails.
- `redis/nanocache.EvictPattern` uses `SCAN`; `modules/cache` uses `KEYS`.

## db/v0.23.0-beta.1
- First release of Nanostack DB: go-jet d0e152c (v2.16.1 + #628) fork with `functional.Option` support, `query`, `filter`, `stmtcache`, `-nullable=option`. See `db/UPSTREAM.md`.

## pkg/functional/v0.23.0-beta.1
- First release as its own module. Adds `Option.Scan` and JSON encoding.
