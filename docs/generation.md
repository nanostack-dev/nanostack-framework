# Generation

Generation remains explicit. The framework standardizes inputs, paths, and validation; it does not hide `oapi-codegen`, migrations, or go-jet.

## OpenAPI

Convention:

- source spec: `cmd/http/openapi.yaml`
- server output: `internal/apigen/gen.go` or the app's chosen generated package
- generated clients stay in app-owned client folders
- cleaned client specs are generated from schema-aware tooling, not ad hoc regex-only transforms

Framework home:

- `gen/oapi` contains config conventions and future helpers
- `cli generate oapi` should orchestrate `oapi-codegen` with visible config files

## Go-Jet

Convention:

- migrations are applied before generation
- generated output goes under `internal/db/gen`
- custom type mappings are explicit, including `text[] -> pq.StringArray`
- database/env names are parameterized by service

Framework home:

- `gen/jet` contains generation configuration conventions
- `cli generate db` should replace duplicated `dbgen.go` and brittle shell generation flows over time

## Replacing Shell Scripts

Existing app scripts such as `generate_anchor.sh` and `generate_echopoint.sh` should be treated as migration sources. The framework path is:

1. Keep current scripts while framework generation is introduced.
2. Move shared generation configuration into `gen/oapi` and `gen/jet`.
3. Add CLI commands that call visible tools and config files.
4. Switch app scripts to thin wrappers or remove them after `anchor` and `echopoint` validation passes.
