# Service Starter

This directory is the future source for `nanostack new service <name>`.

The starter should be intentionally small and should show how to compose:

- `app.New(serviceName)`
- `modules/config`
- `modules/logging`
- `modules/postgres`
- `modules/migrations`
- `modules/httpserver`
- `nanogen/project.go` and `go tool nanostack gen` (the go-jet models from the migrations, the OpenAPI server and clients)

Generated code, OpenAPI specs, migrations, and feature modules remain app-owned after creation.
