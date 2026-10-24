# tracing

OpenTelemetry distributed tracing for Nanostack services. A request that enters through Traefik with a W3C `traceparent` continues as one trace through every framework service it reaches, and every log line carries `trace_id`/`span_id`, so a log row links to its trace.

The package has no fx dependency. [`modules/tracing`](../../modules/tracing/tracing.go) wires it.

## Environment

| Variable | Effect |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Enables export over OTLP/HTTP (e.g. `http://otel-collector:4318`). Unset means a no-op provider. |
| `OTEL_SDK_DISABLED=true`, `OTEL_TRACES_EXPORTER=none` | Forces the no-op provider. |
| `OTEL_TRACES_SAMPLER_ARG` | Ratio for `ParentBased(TraceIDRatio)`; default `1.0`, invalid values fall back to it. A sampled incoming parent is always honoured. |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Resource (`service.name`, `deployment.environment=...`). |
| `SERVICE_NAME`, `SERVICE_VERSION` | Fallbacks for `service.name`/`service.version` when the OTEL variables do not set them. The build-info module version is the last fallback for the version. |

Other `OTEL_EXPORTER_OTLP_*` variables (headers, timeout, compression) are read by the SDK exporter directly. `OTEL_TRACES_SAMPLER` is ignored: the sampler is always parent-based.

## No-op behavior

Without an endpoint, `New` returns a no-op provider and nothing connects anywhere, so local development and tests need no collector. The W3C TraceContext + Baggage propagator is still installed: an incoming `traceparent` stays on the context, logs keep the edge's `trace_id`, and outbound calls keep forwarding it.

When exporting, spans go through a batch span processor with a bounded queue that drops on overflow. An unreachable collector never fails or slows a request; export errors are logged at warn through the otel error handler. `Shutdown` flushes and is bounded by its context (the fx stop timeout).

## Inbound HTTP

```go
router := chi.NewRouter()
router.Use(tracing.RouteMiddleware)          // first: names the span after the chi route
router.Use(requestlog.Contextualize(logger)) // sees the server span
// ... routes
handler := tracing.Middleware()(router)      // outermost
```

`Middleware` starts the server span (continuing `traceparent`) and skips `/health`. `RouteMiddleware` renames it `"METHOD /flows/{flowID}"` and sets `http.route` after routing, so ids in raw paths never become span names. An unmatched request keeps the method-only name. `modules/httpserver` installs both.

## Outbound HTTP

```go
client := tracing.Client(&http.Client{Timeout: 10 * time.Second})
// or: transport := tracing.Transport(baseTransport)
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, anchorURL, nil)
```

Each call gets a client span and a `traceparent` header. Requests must carry the caller's context to join its trace. `Client` copies its argument instead of mutating it.

## Logs

`log.TraceEnricher` (registered by `modules/logging`) adds `trace_id`/`span_id` on every `log.Bind`, including queue jobs whose context carries a span. `requestlog.Contextualize` adds the same fields to the request logger. Both derive from different base loggers, so no line carries a key twice; do not register `log.TraceEnricher` a second time.

## PostgreSQL

`modules/postgres` wraps `lib/pq` with [`otelsql`](https://github.com/XSAM/otelsql). Queries become child spans named by `database/sql` method (`sql.conn.query`, `sql.conn.exec`, `sql.conn.begin_tx`, ...), with `db.system.name=postgresql` and the statement text but never the bind arguments. Pings, session resets and row iteration produce no spans, and a query without a span on its context (migrations, pollers, lock heartbeats) starts no trace.
