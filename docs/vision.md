# Vision

Nanostack Framework should be the best way to build a Nanostack backend service.

The goal is not to hide core technologies such as OpenAPI, `oapi-codegen`, `go-jet`, PostgreSQL, or migrations.
The goal is to make them composable, opinionated, and repeatable so every Nanostack service starts from the same strong baseline.

## Product Definition

Nanostack Framework should provide:

1. An `app/` composition API for service bootstrap.
2. `modules/` for standard lifecycle and dependency-injection integrations.
3. `pkg/` packages for narrow reusable primitives.
4. A standard `gen/` workflow for API and database layers.
5. A `starter/` structure for new services.
6. A `cli/` automation layer for creating, validating, generating, and upgrading services.

## Principles

1. OpenAPI remains the source of truth for HTTP contracts.
2. Generated code remains visible and explicit.
3. Database schema stays migration-driven.
4. SQL stays type-safe and application-owned.
5. Framework abstractions should remove repetition, not remove clarity.
6. Framework packages should be narrowly scoped and predictable.
7. Service modules, reusable packages, generation tooling, and starter files must be treated as separate framework areas.

## Non-Goals

1. Hiding OpenAPI or generated types behind opaque abstractions.
2. Building a magic ORM or hiding SQL semantics.
3. Centralizing product-specific business logic inside the framework.
4. Replacing service architecture decisions that should remain application-owned.

## Desired Outcome

Creating a new Nanostack service should feel like assembling known building blocks, not re-inventing a platform each time.
