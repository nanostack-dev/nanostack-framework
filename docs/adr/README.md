# Architectural decisions

The authoritative historical records are the [dated decision log](../decision-log.md). Preserve those dates, rationale and supersession notes; this directory adds an entry point without renumbering or inventing past ADRs.

New consequential decisions may use `NNNN-<decision>.md`, starting at `0001`, with status, context, decision, alternatives and consequences. Reference relevant dated entries. Record a reversal as a new superseding decision, and update current behavior separately in [technical documentation](../technical/architecture.md).

## Records

- [0001: Keyset cursor pagination for searches](0001-keyset-cursor-pagination.md)

### Feature architecture

Read together through the [feature architecture guide](../feature-architecture.md). Each record is proposed unless marked accepted.

- [0002: One package per feature, plus a pure domain package](0002-one-package-per-feature.md)
- [0003: Domain types carry the rules](0003-domain-types-carry-the-rules.md)
- [0004: One action per write, one Queries type per feature](0004-one-action-per-write.md)
- [0005: Concrete repositories per aggregate, explicit mapping](0005-concrete-repositories.md)
- [0006: Actions own the transaction](0006-actions-own-the-transaction.md)
- [0007: Side effects leave through the outbox](0007-side-effects-leave-through-the-outbox.md)
- [0008: Errors are wrapped on the way up and logged once](0008-errors-are-wrapped-and-logged-once.md)
- [0009: Handlers only translate](0009-handlers-only-translate.md)
- [0010: Wiring is plain Go](0010-wiring-is-plain-go.md) (accepted)
- [0011: HTTP tests specify, table tests prove rules](0011-http-tests-specify-table-tests-prove-rules.md)
- [0012: Conventions are enforced by lint, not review](0012-conventions-are-enforced-by-lint.md)
