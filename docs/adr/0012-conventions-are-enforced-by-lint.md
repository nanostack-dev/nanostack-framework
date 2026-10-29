# 0012: Conventions are enforced by lint, not review

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

The decisions in this guide only help if every feature follows them. Echopoint and anchor already run depguard, funlen and gocognit, plus ruleguard rules in `gorules/`. Today they don't encode any layering rule.

## Decision

- **Names.** The action file is `<verb>_<noun>.go` (`restore_flow_version.go`), the type is `RestoreFlowVersion`, and the test is `<noun>_<verb>_test.go`. Every action has a doc comment stating its rule and its side effects.
- **Budgets.** Warn when a non-generated file passes about 400 lines, or a function passes the existing `funlen` limit. A feature package above about 3,000 lines is a prompt to look for a pure subpackage ([0002](0002-one-package-per-feature.md)).
- **Import rules in depguard:**

  ```yaml
  depguard:
    rules:
      domain-is-pure:
        files: ["**/internal/domain/**"]
        deny:
          - pkg: database/sql
            desc: domain does no I/O (ADR 0003)
          - pkg: echopoint/internal/feature
            desc: domain must not depend on features
          - pkg: echopoint/internal/apigen
            desc: HTTP types stay in handlers (ADR 0009)
      features-do-not-import-features:
        files: ["**/internal/feature/**", "!**/internal/feature/flows/**"]
        deny:
          - pkg: echopoint/internal/feature/flows
            desc: declare a narrow interface in the consumer (ADR 0004)
  ```

- **Complete `Deps`.** `exhaustruct`, limited to types named `Deps`, fails a struct literal that leaves a dependency out ([0010](0010-wiring-is-plain-go.md)).
- **A ruleguard rule** flags `log.Error(...)` followed by `return err` ([0008](0008-errors-are-wrapped-and-logged-once.md)).

## Alternatives

- **Leave it to review.** Each reviewer re-checks the same rules, and they drift between services.

## Consequences

- A layering violation fails CI with a message that links to its ADR.
- The rules are per service, because import paths differ. The framework can ship a template configuration in [`starter/`](../../starter/README.md).
