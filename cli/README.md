# cli

Developer workflows for creating, validating, generating, and upgrading Nanostack services.

CLI commands should orchestrate explicit framework tooling rather than hiding OpenAPI, migrations, or go-jet.

Initial command model:

- `nanostack new service <name>` creates a service from `starter/`.
- `nanostack generate oapi` runs visible oapi-codegen configs.
- `nanostack generate db` applies migrations in a generation database and runs go-jet.
- `nanostack doctor` validates framework conventions.
- `nanostack upgrade` assists compatibility import migration.
