# starter

Canonical new-service baseline files.

Starter files should demonstrate framework conventions while keeping service-specific code easy to replace.

The starter baseline should include:

- `cmd/http/openapi.yaml`
- `cmd/app` using `app.New(serviceName)`
- `internal/apigen` for generated server code
- `internal/db/gen` for generated go-jet code
- `migrations/`
- `application.yaml`
- CT harness entry points using `pkg/testkit/ct` once available
