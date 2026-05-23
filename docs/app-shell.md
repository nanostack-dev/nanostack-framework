# App Shell

The first app-shell layer is intentionally small.

## Fluent App API

`app.New(serviceName)` collects explicit `fx.Option` values. `With` is for framework or infrastructure modules. `Use` is for application-owned feature modules. `Populate` keeps the underlying `fx.Populate` behavior visible for tests.

## HTTP Server

`modules/httpserver` owns the generic server lifecycle. Applications provide OpenAPI bytes, generated handler registration, route configuration, auth middleware, CORS predicates, validator bypass rules, and health extras.

## PProf

`modules/pprof` is opt-in, defaults to `127.0.0.1:6060`, and uses `ENABLE_PPROF` unless configured otherwise.

## Queue Workers

`pkg/queueworker` provides cancellation and wait primitives for durable queue workers. pgqueue-specific wiring belongs under `modules/pgqueue` once app adoption needs it.

Retry policy, queue names, payloads, and idempotency stay application-owned.
