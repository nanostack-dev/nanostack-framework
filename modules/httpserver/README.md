# httpserver

Generic HTTP server lifecycle module.

Applications provide the product-owned pieces: OpenAPI bytes, generated handler registration, middleware adapters, CORS predicates, validator bypass rules, route registration, and health extras.

The module owns the reusable shell: chi router creation, CORS wiring, optional OpenAPI request validation, `/openapi.yaml`, `/health`, strict-handler API error rendering, server timeouts, and graceful shutdown.

Apps can still provide narrow hooks for app-owned legacy error adapters or SSE route detection when those policies are not generic enough for the framework to infer.
