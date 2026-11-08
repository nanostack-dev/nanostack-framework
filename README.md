# Nanostack Framework

Nanostack Framework is the baseline for building Nanostack backend services.

It aims to standardize the parts that are repeatedly rebuilt across services:

- app/bootstrap wiring
- OpenAPI-first server generation
- go-jet database generation and query conventions
- migrations and database lifecycle
- observability, health, and error handling
- service project scaffolding and upgrade paths

This repository is private while the architecture, naming, and boundaries are being defined.

## Layout

New code lives in four trees and three layers: `core/`, then `http/`, then `gen/` with `cmd/nano`. A package imports only its own layer and the layers below:

1. `core/`: runtime: `nano` (lifecycle, Main), `config`, `log`, `fault`, `validate`, `secret`.
2. `http/`: the HTTP boundary: `problem`, `nanohttp`.
3. `gen/`: generators behind `nano gen` (`wire`, `config`, `errors`, `validate`).
4. `cmd/nano`: the CLI.

`pkg/` and `modules/` hold the legacy Fx stack until services migrate.

## Initial Direction

This list is historical: the original plan, kept for context. The layout above is current.

The framework was expected to cover six explicit areas:

1. `app/`: fluent service composition API.
2. `modules/`: lifecycle and dependency-injection integrations.
3. `pkg/`: narrowly scoped reusable primitives.
4. `gen/`: OpenAPI and go-jet generation tooling.
5. `cli/`: developer workflows for new services, generation, doctor checks, and upgrades.
6. `starter/`: a canonical new-service baseline.

The current `nanostack-shared` repository is expected to become a compatibility bridge while its cleaned contents move into bounded framework packages and modules.

## Documents

For standalone development, start with [AGENTS.md](AGENTS.md), [CONTEXT.md](CONTEXT.md) and the [documentation index](docs/README.md). [Implemented architecture](docs/technical/architecture.md) distinguishes current packages from the original direction below.

- `docs/vision.md`: framework intent and goals
- `docs/common-work-inventory.md`: repeated work currently done by apps
- `docs/architecture.md`: the original component direction, kept as history
- `docs/boundaries.md`: which tree owns what, and what stays in app code
- `docs/privacy.md`: repository history and publication strategy
- `docs/roadmap.md`: phased execution plan
