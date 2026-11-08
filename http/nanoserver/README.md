# http/nanoserver

The nano module that serves the service's HTTP handler. It replaces the server part of the fx module `modules/httpserver`, and the goroutine, signal handler and `Fatal` call that services wrote around it.

- **Provides:** `*Server` from `NewServer(Config, http.Handler, *slog.Logger)`. The service provides the `http.Handler`: it keeps its router, its middleware order (tracing, request logging, CORS, validation) and its `/health` route.
- **Lifecycle:** `Server` is a `Runner`. `Run` listens, serves until the App stops, then stops accepting connections and waits up to `shutdown_timeout` for in-flight requests. After that it closes the remaining connections, logs their count at Warn and returns nil: a stream that outlives the drain does not fail the shutdown. A listen failure stops the App.
- **Streams:** a long-lived handler (server-sent events, long poll) selects on `nanoserver.Draining(r.Context())` and ends its response when the drain starts, so the client reconnects to another instance before `shutdown_timeout`.
- **Config** (section `http`), with the defaults of the fx module: `host` (all interfaces), `port` (8080), `read_header_timeout` (30s), `read_timeout` (60s), `write_timeout` (60s), `idle_timeout` (120s), `shutdown_timeout` (5s). Keep `shutdown_timeout` below `APP_STOP_TIMEOUT` minus `APP_DRAIN_DELAY`: the drain delay is counted inside the stop timeout. A zero or negative timeout takes its default, so a `Config{}` built by hand is never a server without timeouts; `DefaultConfig()` also sets the port.

## `/health`

`Readiness` reports the App's readiness to the legacy `/health` handler of `pkg/health`, which a layered package may not import. It has the method set of `health.Checker`:

```go
registry := health.NewRegistry()
registry.Register(nanoserver.NewReadiness(app))
mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Registry: registry}))
```

The body then gets a `"readiness"` key. A service whose `/health` body is pinned, such as by a gate flow, uses `ReadinessMiddleware` instead. It sets the status and leaves the body byte for byte the same:

```go
router.With(nanoserver.ReadinessMiddleware(app)).Get("/health", healthHandler)
```

While the App starts or drains, or a `HealthChecker` fails, a 2xx or 3xx status of the handler, or no status at all, becomes 503. An error status of the handler stays.

See [docs/modules.md](../../docs/modules.md) for the migration from fx.
