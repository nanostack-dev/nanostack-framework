# 0019: net/http ServeMux replaces chi

- Status: proposed
- Date: 2026-11-07

## Context

`go-chi/chi/v5` routes requests in `modules/httpserver`, which builds a `chi.Mux` and exposes it through `Options.ConfigureRouter func(*chi.Mux)` and `Options.Handler func(*chi.Mux) http.Handler`. `pkg/tracing` reads the chi route context to name spans by route pattern, because raw paths carry ids (2026-10-24 "OpenTelemetry Tracing In The Framework" in the [decision log](../decision-log.md)). `go-chi/cors` handles CORS.

Since Go 1.22, `http.ServeMux` matches methods and path wildcards (`GET /invoices/{id}`), and since Go 1.23 `http.Request.Pattern` holds the matched pattern. oapi-codegen's `std-http-server` target registers handlers on a `ServeMux`.

## Decision

`http.ServeMux` is the framework router.

- The HTTP module builds a `*http.ServeMux`, and the generated strict server registers on it. Options that took `*chi.Mux` take `*http.ServeMux`.
- Middleware is `func(http.Handler) http.Handler`, composed by the framework in a fixed order.
- Span names and request log fields use `r.Pattern`.
- `go-chi/chi` is removed from every `go.mod`. `go-chi/cors` does not depend on the chi router, so it does not keep chi alive; whether CORS stays on it or moves to a framework middleware is decided with the problem writer work.

## Alternatives

- **Keep chi.** Route groups and inline middleware stacks are its remaining advantages, and composing handlers gives both. It is a dependency that duplicates the standard library.
- **gorilla/mux.** Regular-expression routes the contract does not need, and a project with a history of maintenance gaps.
- **echo, gin, httprouter.** Their own handler or context types, which the generated standard `net/http` strict server does not use.
- **ServeMux behind a framework router interface.** An indirection with only one implementation.

## Consequences

- One dependency fewer, and the router is the one every Go developer and agent already knows.
- Breaking for products that configure the router: `ConfigureRouter` and `Handler` change type.
- `ServeMux` panics at registration on conflicting patterns, so an ambiguous route fails at startup instead of shadowing another.
- Path wildcards have no regular-expression constraints. The generated binding and validation check path parameters.
- `ServeMux` answers 405 with an `Allow` header for a known path with the wrong method. A trailing slash and `{$}` follow its rules, which differ from chi's; contract tests cover the routes products expose.
