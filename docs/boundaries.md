# Boundaries

Nanostack Framework removes repeated service shell code without absorbing product semantics.

## Repository Areas

- `app/` owns fluent service assembly on top of explicit FX options.
- `modules/` owns lifecycle and dependency-injection wiring for reusable infrastructure.
- `pkg/` owns small reusable primitives that are useful without FX.
- `gen/` owns OpenAPI and go-jet generation helpers and configuration conventions.
- `cli/` owns developer workflows that call into `gen/`, `starter/`, and validation checks.
- `starter/` owns new-service baseline files.
- `docs/` owns architecture, migration, and decision records.

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
