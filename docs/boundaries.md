# Boundaries

Nanostack Framework removes repeated service shell code without absorbing product semantics.

## Repository Areas

New code lives in three layers. A package imports only its own layer and the layers below, and never the legacy trees (`pkg/`, `modules/`, `app/`). [Implemented architecture](technical/architecture.md#layers) states the rule and how it is enforced.

- `core/` owns the runtime that services and generated code call: `core/nano` (lifecycle, `Main`, admin probes, manifest declarations; `core/nano/project` holds the project declaration that `nano gen` reads), `core/config` (configuration binding), `core/log` (slog logging and redaction), `core/fault` (the problem contract of error catalogues), `core/validate` (the runtime of request validators) and `core/secret` (the one secret type and every credential-redaction rule).
- `http/` owns the HTTP boundary between `net/http` and generated handlers: strict decoding (`http/nanohttp`) and RFC 9457 problem responses (`http/problem`). [The HTTP boundary](http-boundary.md) is its contract.
- `gen/` is its own module. It owns the generators behind `nano gen` (`gen/api`, which links oapi-codegen, `gen/config`, `gen/errors`, `gen/validate`, `gen/wire`), the pipeline that runs them in order (`gen/pipeline`), the generator contract (package `gen`), and go-jet's conventions (`gen/jet`).
- `gen/cmd/nano` owns the CLI, `nano`. Today it runs `nano gen`, the pipeline, and `nano gen <name>`, one generator.
- `docs/` owns architecture, migration, and decision records.

Legacy trees, which hold the Fx stack until services migrate:

- `app/` owns fluent service assembly on top of explicit FX options.
- `modules/` owns lifecycle and dependency-injection wiring for reusable infrastructure.
- `pkg/` owns small reusable primitives that are useful without FX.

Direction only, with no runnable code yet:

- `cli/` describes developer workflows; new commands join `nano` in `gen/cmd/nano`.
- `starter/` describes new-service baseline files.

## Module Rules

`modules/` packages may depend on FX and external service clients when their purpose is lifecycle wiring. They should stay thin and make app-specific behavior injectable.

Examples that belong in `modules/`:

- config loader lifecycle wiring
- logging provider wiring
- postgres connection lifecycle
- redis cache lifecycle
- migration startup hooks
- HTTP server lifecycle
- pprof lifecycle
- queue worker lifecycle helpers

## Package Rules

`pkg/` packages should not depend on FX. If a package needs FX, it probably belongs under `modules/` or should expose a primitive in `pkg/` with a thin module wrapper.

Examples that belong in `pkg/`:

- prefixed KSUID generation
- secret hashing, masking, and encryption
- API error models
- health handler primitives
- search request/result DTOs
- go-jet query helpers
- request logging middleware
- transaction helpers

## App-Owned Semantics

The framework must not own product decisions.

Examples that stay in apps:

- Anchor tenant/product/resource authorization
- Echopoint organization resolution and route policies
- OpenAPI schemas and generated application packages
- queue names, payload schemas, and retry/idempotency policy
- Redis stream key names and terminal event definitions
- product-specific test fixtures and fluent scenario builders
