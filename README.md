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

## Initial Direction

The framework is expected to cover six explicit areas:

1. `app/`: fluent service composition API.
2. `modules/`: lifecycle and dependency-injection integrations.
3. `pkg/`: narrowly scoped reusable primitives.
4. `gen/`: OpenAPI and go-jet generation tooling.
5. `cli/`: developer workflows for new services, generation, doctor checks, and upgrades.
6. `starter/`: a canonical new-service baseline.

The current `nanostack-shared` repository is expected to become a compatibility bridge while its cleaned contents move into bounded framework packages and modules.

## Documents

- `docs/vision.md`: framework intent and goals
- `docs/common-work-inventory.md`: repeated work currently done by apps
- `docs/architecture.md`: proposed framework component boundaries
- `docs/boundaries.md`: rules for deciding what belongs in `modules/`, `pkg/`, or app code
- `docs/privacy.md`: repository history and publication strategy
- `docs/roadmap.md`: phased execution plan
