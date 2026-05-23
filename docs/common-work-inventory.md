# Common Work Inventory

This document inventories repeated work currently performed across `anchor`, `echopoint`, and `nanostack-shared`.

## App / Bootstrap

Repeated across applications:

- FX application bootstrap in `cmd/app/app.go`
- config loader injection into app modules
- postgres connection lifecycle
- redis cache wiring with fallback behavior
- migration startup hooks
- pglock startup wiring
- sentry initialization and middleware registration
- health endpoint registration
- request logging middleware wrapping
- server startup/shutdown lifecycle hooks

Examples:

- `anchor/apps/anchor/cmd/app/app.go`
- `echopoint/cmd/app/app.go`
- `nanostack-shared/fxmodules/*`

## HTTP Server Composition

Repeated patterns:

- embed `cmd/http/openapi.yaml`
- load server config from shared config loader
- configure `chi`
- install CORS
- install request validator from `oapi-codegen` middleware
- skip validation on health/docs/SSE endpoints
- register docs routes
- register health routes
- start `http.Server` with graceful shutdown

Examples:

- `anchor/apps/anchor/cmd/http/module.go`
- `anchor/apps/anchor/cmd/http/server.go`
- `echopoint/cmd/http/module.go`
- `echopoint/cmd/http/server.go`

## OpenAPI Generation

Repeated patterns:

- service server codegen config
- client codegen config
- generated code locations
- OpenAPI cleaning / preprocessing steps
- strict server handler conventions

Examples:

- `anchor/apps/anchor/codegen.yaml`
- `anchor/apps/anchor/client-codegen.yaml`
- `anchor/apps/anchor/client-types-codegen.yaml`
- `echopoint/codegen.yaml`
- `echopoint/client-codegen.yaml`
- `anchor/apps/anchor/generate_anchor.sh`
- `echopoint/generate_echopoint.sh`

## Database Generation

Repeated patterns:

- `dbgen.go` scripts
- same `go-jet` generation pattern
- same `text[]` handling override
- same admin-db bootstrap logic
- same environment-parsing helpers

Examples:

- `anchor/apps/anchor/dbgen.go`
- `echopoint/dbgen.go`

## Shared Domain Support

Repeated patterns currently handled by `nanostack-shared`:

- validation and structured API error modeling
- prefixed KSUID generation
- secret masking and secret encryption helpers
- pagination and search request types
- Jet query helper functions
- middleware and health primitives

Hot spots by import count:

- `toolkit`
- `toolkit/search`
- `fxmodules/config`
- `fxmodules/cache`

These are likely the highest-leverage framework extraction points.

## Test / Developer Workflows

Repeated patterns:

- contract test package structure under `cmd/it/ct`
- generated clients for CT
- local dev scripts and agent workflows
- env/config conventions for local service startup

Examples:

- `anchor/apps/anchor/cmd/it/ct/...`
- `echopoint/cmd/it/ct/...`
- `echopoint/docs/agent-worktree-dev.md`

## What The Framework Should Probably Own

1. App bootstrap and lifecycle modules.
2. Standard HTTP server composition helpers.
3. OpenAPI generation conventions.
4. Go-Jet generation conventions and helpers.
5. A starter service layout.
6. CLI tasks for generate, validate, and scaffold.

## What The Framework Should Probably Not Own

1. Product-specific domain logic.
2. Service-specific auth decisions.
3. Service-specific middleware exceptions.
4. Service-specific OpenAPI schemas and handlers.
