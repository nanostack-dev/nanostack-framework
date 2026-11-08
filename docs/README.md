# Framework documentation

Start with [agent rules](../AGENTS.md) and [canonical vocabulary](../CONTEXT.md). The repository supports independent checkout and development.

## Current system

- [Implemented architecture](technical/architecture.md) and [ownership boundaries](boundaries.md).
- [Functional values and collections](../pkg/functional/README.md).
- [Transactions and queries](../pkg/db/transactor/README.md), [SQL error classification](../pkg/db/pgerr/README.md), [Jet expressions](../pkg/jetx/README.md).
- [HTTP boundary](http-boundary.md): strict decoding, generated validation and RFC 9457 problem responses.
- [HTTP lifecycle](../modules/httpserver/README.md), [request logging](../pkg/httputil/requestlog/README.md), [legacy error envelope](../pkg/httputil/errorenvelope/README.md), [distributed tracing](../pkg/tracing/README.md), [typed cache](../modules/cache/README.md), [shared state, mutex and pub/sub on Redis](../pkg/redisx/README.md), [queue lifecycle](../modules/pgqueue/README.md).
- [Generated wiring: manifests, lifecycle, Overrides and `nano gen wire`](generation.md#wiring-nano-gen-wire), and the [generator contract](../gen/README.md).
- [Nano modules](modules.md): the generated-wiring replacements of the fx modules, their contract, and how a service moves one module at a time.
- [Workflow bridge](../pkg/workflow/README.md) and [contract-test harness](../pkg/testkit/ct/README.md).
- [Real Redis or Valkey for tests](../pkg/redisx/README.md#testing).

## Development and operations

- [Setup](development/setup.md), [testing](development/testing.md), [troubleshooting](development/troubleshooting.md).
- [Lint standard](../lint/README.md) shared by every service, with its [research](research/golangci-lint-standard.md), an [architecture review](research/lint-standard-architecture.md) and the [generic-method smoke test](../lint/README.md#generic-methods-and-the-smoke-test).
- [Publishing and consumer upgrades](runbooks/deployment.md), [release steps](../RELEASING.md), [changelog](../CHANGELOG.md), [rollback](runbooks/rollback.md).
- [Architectural decisions](adr/README.md), preserving the [dated decision log](decision-log.md).

## Direction and proposals

These existing documents include intended future capabilities; the implemented architecture above distinguishes current code from plans.

- [Vision](vision.md), [original architecture direction](architecture.md), [roadmap](roadmap.md), [common-work inventory](common-work-inventory.md).
- Proposed framework direction: [ADRs 0013 to 0021](adr/README.md) (generated wiring, one repository, the go-jet `db` fork, generated validation, strict JSON decoding, slog, ServeMux, db/migrate, queue hardening).
- [Service assembly direction](app-shell.md), [generation conventions](generation.md), [testkit scope](testkit.md), [history and publication strategy](privacy.md).
- [CLI direction](../cli/README.md), [starter direction](../starter/README.md) and [service starter](../starter/service/README.md).

Package guides remain beside their owners. Add optional research and postmortem folders when actual findings or incidents warrant them.
