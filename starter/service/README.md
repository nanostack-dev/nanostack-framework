# Service Starter

This directory is the future source for `nanostack new service <name>`.

The starter should be intentionally small and should show how to compose:

- `app.New(serviceName)`
- `modules/config`
- `modules/logging`
- `modules/postgres`
- `modules/migrations`
- `modules/httpserver`
- `gen/oapi`
- `gen/jet`

Generated code, OpenAPI specs, migrations, and feature modules remain app-owned after creation.
