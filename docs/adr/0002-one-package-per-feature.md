# 0002: One package per feature, plus a pure domain package

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

The two services use different layouts:

- **Anchor is mostly flat.** One feature change spans `internal/service`, `internal/repository` and `internal/api`. Its ADR-0007 started moving new work into slices such as `internal/license`.
- **Echopoint uses `internal/feature/<plural>`.** Its legacy `internal/service`, `internal/repository` and `internal/mapper` packages remain.

A newcomer has to learn both layouts, and a reviewer can't predict where a change lives.

## Decision

Each feature is one Go package under `internal/feature/<plural>`. Rules without I/O live in `internal/domain/<entity>` ([0003](0003-domain-types-carry-the-rules.md)). A subpackage exists only for a pure part with its own test matrix, such as anchor's `license/rules`.

```
cmd/app/main.go                 run(ctx) → app.New → app.Run           (0010)
internal/
  app/app.go                    the only cross-feature wiring           (0010)
  domain/flow/                  types + rules, no I/O                   (0003)
    flow.go  definition.go  definition_test.go
  feature/flows/
    flows.go                    Deps + New(): feature wiring
    handler.go  api_mapper.go   HTTP mapping only                       (0009)
    queries.go                  reads                                   (0004)
    publish_flow.go             one write = one file                    (0004)
    restore_flow_version.go
    lookups.go                  requireFlow, requireVersion
    repository.go  db_mapper.go                                         (0005)
    errors.go                   fault constructors                      (0008)
cmd/it/ct/flow_version_restore_test.go   HTTP spec per operation        (0011)
```

## Alternatives

- **Flat layers** (anchor today). This makes packages by kind, not by feature, so they grow without bound and every change touches several directories.
- **Hexagonal subpackages per feature** ([Wild Workouts](https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example/tree/master/internal/trainings): `adapters/ app/command app/query domain/ ports/`). Every layer has to export to the next, which multiplies packages and import aliases. Worth it only for a feature that is a product in itself.
- **A root domain package with adapter subpackages** ([Ben Johnson](https://www.gobeyond.dev/standard-package-layout/)). Good for small apps and libraries. With 20+ features, the root package becomes the next god package.

## Consequences

- Every feature has the same shape, so a reviewer knows where each kind of code lives.
- Go keeps server logic in `internal/` so it can be refactored freely ([go.dev](https://go.dev/doc/modules/layout)). Google's style guide asks to avoid both thousand-line files and "many tiny files", and has no "one type, one file" rule ([best practices](https://google.github.io/styleguide/go/best-practices#package-size)). One package with one file per operation sits between those two.
- Packages can grow large, so [0012](0012-conventions-are-enforced-by-lint.md) sets budgets.
- Anchor's single generated API struct must be split per feature first ([0009](0009-handlers-only-translate.md)).
