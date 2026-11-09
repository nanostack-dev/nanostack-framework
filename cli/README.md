# cli

Developer workflows for creating, validating, generating, and upgrading Nanostack services.

CLI commands should orchestrate explicit framework tooling rather than hiding OpenAPI, migrations, or go-jet.

> **Superseded in part.** The CLI that exists is `nanostack` ([`gen/cmd/nanostack`](../gen/cmd/nanostack)), whose `nanostack gen <name>` runs the framework's generators. The `nanostack generate oapi|db` commands below are the original direction and are not built; new commands join `nanostack`.

Initial command model:

- `nanostack new service <name>` creates a service from `starter/`.
- `nanostack generate oapi` runs visible oapi-codegen configs.
- `nanostack generate db` applies migrations in a generation database and runs go-jet.
- `nanostack doctor` validates framework conventions.
- `nanostack upgrade` assists compatibility import migration.
