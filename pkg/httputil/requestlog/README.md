# requestlog

HTTP request logging plus request-scoped log context for Nanostack Go services.

## Pieces

- `New` / `NewFromEnv` — middleware that logs one `incoming request` line per
  request (method, path, status, duration, optional body) and a `server error`
  line for 5xx responses.
- `Contextualize` — middleware that establishes a per-request correlation id and
  a request-scoped `zerolog.Logger` on the context.
- `From(ctx)` — fetch the request-scoped logger (disabled no-op when absent).
- `RequestIDFromContext(ctx)` — fetch the correlation id (empty when absent).

## What Contextualize does

For each request it:

- reuses an inbound `X-Request-Id` header or mints a KSUID-backed id;
- stores the id on the context and echoes it on the `X-Request-Id` response
  header;
- derives a child logger carrying `request_id`, `method` and `path` and stores
  it on the context.

Downstream code logs through `requestlog.From(ctx)` and every line inherits
those fields. Middleware that learns more about the request later (for example
an auth layer resolving an org id) enriches the same logger in place:

```go
requestlog.From(r.Context()).UpdateContext(func(c zerolog.Context) zerolog.Context {
    return c.Str("org_id", orgID)
})
```

`org_id` then appears on every subsequent log line for that request, including
the `incoming request` summary, with no changes at the call sites.

## Wiring order

`Contextualize` must run before anything that reads or enriches the request
logger:

```
Contextualize(base)   // seeds request_id + request-scoped logger
  └─ auth middleware   // From(ctx).UpdateContext(... org_id ...)
       └─ New(base, …)  // reuses the context logger; drops its own method/path
            └─ handlers // requestlog.From(ctx).Info()...
```

When `Contextualize` ran, `New` reuses the context logger (already carrying
`request_id`, `method`, `path`) and omits its own route fields to avoid
duplicate keys. Standalone consumers that skip `Contextualize` keep the original
behavior: `New` logs method and path from its injected logger.
```
