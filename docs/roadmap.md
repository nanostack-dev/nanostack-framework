# Roadmap

## Phase 0: Define The Framework

1. Agree on scope and naming.
2. Confirm repository strategy.
3. Define the `app`/`modules`/`pkg`/`gen`/`starter`/`cli` split.
4. Define migration policy from `nanostack-shared`.

## Phase 1: Bounded Packages And Modules

1. Refactor `nanostack-shared` into bounded `pkg/` packages and `modules/` lifecycle integrations.
2. Keep compatibility imports while migrating applications.
3. Validate against `anchor` and `echopoint` contract tests and full builds.

## Phase 2: Generation Standardization

1. Extract standard OpenAPI server generation config.
2. Extract standard client generation config.
3. Extract standard `go-jet` generation flow.
4. Reduce duplicated shell scripts and `dbgen.go` logic.

## Phase 3: Starter Service

1. Define canonical folder layout for a Nanostack service.
2. Provide starter server wiring using `app/` and `modules/` conventions.
3. Provide starter middleware and health wiring.
4. Provide example codegen and migration setup.

## Phase 4: CLI

1. `new service`
2. `generate`
3. `doctor`
4. `upgrade`

## Initial Success Criteria

The framework is succeeding when:

1. A new backend service can be scaffolded with minimal manual wiring.
2. OpenAPI and database generation are standardized through `gen/` and `cli/` workflows.
3. `anchor` and `echopoint` consume clearer packages and modules with less duplication.
4. Framework boundaries are explicit enough to keep the repository clean over time.
