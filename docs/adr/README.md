# Architectural decisions

The authoritative historical records are the [dated decision log](../decision-log.md). Preserve those dates, rationale and supersession notes; this directory adds an entry point without renumbering or inventing past ADRs.

New consequential decisions may use `NNNN-<decision>.md`, starting at `0001`, with status, context, decision, alternatives and consequences. Reference relevant dated entries. Record a reversal as a new superseding decision, and update current behavior separately in [technical documentation](../technical/architecture.md).

## Records

- [0001: Keyset cursor pagination for searches](0001-keyset-cursor-pagination.md)

Numbers 0002 to 0012 are taken by the feature-architecture records in the open [PR #59](https://github.com/nanostack-dev/nanostack-framework/pull/59).

Proposed on 2026-11-07, from the framework plan's phase 0. Each stays proposed until its phase starts.

- [0013: Generated wiring replaces fx](0013-generated-wiring-replaces-fx.md), superseding PR #59's 0010 for graph assembly
- [0014: One repository with nested modules](0014-one-repository-with-nested-modules.md)
- [0015: Fork go-jet as the db module (multi-dialect)](0015-fork-go-jet-as-the-db-module.md)
- [0016: Generated request validation replaces kin-openapi at runtime](0016-generated-request-validation.md)
- [0017: Strict encoding/json/v2 decoding at the HTTP boundary](0017-strict-json-v2-decoding.md)
- [0018: slog is the only logging API](0018-slog-only-logging.md)
- [0019: net/http ServeMux replaces chi](0019-servemux-replaces-chi.md)
- [0020: Framework-owned db/migrate replaces golang-migrate](0020-framework-owned-db-migrate.md)
- [0021: Harden pgkit/queue using River's design](0021-harden-pgkit-queue.md)

Accepted on 2026-11-07, with the error catalogue generator:

- [0022: Error catalogues are declared as a Go interface, with explicit codes](0022-error-catalogue-from-go-interface.md)

Accepted on 2026-11-08, with the `nano gen` pipeline:

- [0023: `nano gen` runs every generation step, in a fixed order, from one declaration](0023-nano-gen-pipeline.md)
