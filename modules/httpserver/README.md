# httpserver

Generic HTTP server lifecycle module.

Applications provide the product-owned pieces: OpenAPI bytes, generated handler registration, middleware adapters, CORS predicates, validator bypass rules, route registration, and health extras.

The module owns the reusable shell: chi router creation, CORS wiring, optional OpenAPI request validation, `/openapi.yaml`, `/health`, strict-handler API error rendering, server timeouts, and graceful shutdown.

Apps can still provide narrow hooks for app-owned legacy error adapters or SSE route detection when those policies are not generic enough for the framework to infer.

A strict handler that fails because the client disconnected (`context.Canceled` in the error chain, or a cancelled request context) is logged at warn and answered with `499` (`StatusClientClosedRequest`), so the access log does not record a 500. `context.DeadlineExceeded` is not treated this way: the framework sets no request deadline, so an expired one is the service's own timeout and stays a 500.
