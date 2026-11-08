# http/nanoserver

The nano module that serves the service's HTTP handler. It replaces the server part of the fx module `modules/httpserver`, and the goroutine, signal handler and `Fatal` call that services wrote around it.

- **Provides:** `*Server` from `NewServer(Config, http.Handler, *slog.Logger)`. The service provides the `http.Handler`: it keeps its router, its middleware order (tracing, request logging, CORS, validation) and its `/health` route.
- **Lifecycle:** `Server` is a `Runner`. `Run` listens, serves until the App stops, then stops accepting connections and waits up to `shutdown_timeout` for in-flight requests. After that it closes the remaining connections. A listen failure stops the App.
- **Config** (section `http`), with the defaults of the fx module: `host` (all interfaces), `port` (8080), `read_header_timeout` (30s), `read_timeout` (60s), `write_timeout` (60s), `idle_timeout` (120s), `shutdown_timeout` (5s). Keep `shutdown_timeout` below `APP_STOP_TIMEOUT`.

## `/health`

`Readiness` reports the App's readiness to the legacy `/health` handler of `pkg/health`, which a layered package may not import. It has the method set of `health.Checker`:

```go
registry := health.NewRegistry()
registry.Register(nanoserver.NewReadiness(app))
mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Registry: registry}))
```

See [docs/modules.md](../../docs/modules.md) for the migration from fx.
