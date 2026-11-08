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

New code lives in layered trees. A package imports only its own layer and the layers below; `internal/layers` and depguard check it:

0. `pkg/functional`: the leaf layer, its own module with no framework dependency. Every layer may import it.
1. `core/`: the runtime: `nano` (lifecycle, Main), `config`, `log`, `logging`, `fault`, `validate`, `secret`.
2. Integrations: `http/` (`problem`, `nanohttp`, `nanoserver`, `nanoapisec`, `nanopprof`), `pg/` (`nanopg`, `nanomigrate`, `nanolock`), `redis/nanocache` and `otel/nanotrace`. The nano modules are listed in [docs/modules.md](docs/modules.md).
3. Tooling: `gen/` (the generators behind `nano gen`: `wire`, `config`, `errors`, `validate`) and `cmd/nano`, the CLI.

`db/` is Nanostack DB, the go-jet fork, in its own module ([db/UPSTREAM.md](db/UPSTREAM.md)). `pkg/`, `modules/` and `app/` hold the legacy Fx stack until the services migrate; the layered trees do not import them, except the primitives listed in `internal/layers.AllowedLegacy`. [docs/technical/architecture.md](docs/technical/architecture.md) describes the architecture.

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
