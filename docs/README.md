# Framework documentation

Start with [agent rules](../AGENTS.md) and [canonical vocabulary](../CONTEXT.md). The repository supports independent checkout and development.

## Current system

- [Implemented architecture](technical/architecture.md) and [ownership boundaries](boundaries.md).
- [Functional values and collections](../pkg/functional/README.md).
- [Transactions and queries](../pkg/db/transactor/README.md), [SQL error classification](../pkg/db/pgerr/README.md), [Jet expressions](../pkg/jetx/README.md).
- [HTTP lifecycle](../modules/httpserver/README.md), [request logging](../pkg/httputil/requestlog/README.md), [typed cache](../modules/cache/README.md), [queue lifecycle](../modules/pgqueue/README.md).
- [Workflow bridge](../pkg/workflow/README.md) and [contract-test harness](../pkg/testkit/ct/README.md).

## Development and operations

- [Setup](development/setup.md), [testing](development/testing.md), [troubleshooting](development/troubleshooting.md).
- [Publishing and consumer upgrades](runbooks/deployment.md), [rollback](runbooks/rollback.md).
- [Architectural decisions](adr/README.md), preserving the [dated decision log](decision-log.md).

## Direction and proposals

These existing documents include intended future capabilities; the implemented architecture above distinguishes current code from plans.

- [Vision](vision.md), [original architecture direction](architecture.md), [roadmap](roadmap.md), [common-work inventory](common-work-inventory.md).
- [Service assembly direction](app-shell.md), [generation conventions](generation.md), [testkit scope](testkit.md), [history and publication strategy](privacy.md).
- [CLI direction](../cli/README.md), [starter direction](../starter/README.md) and [service starter](../starter/service/README.md).

Package guides remain beside their owners. Add optional research and postmortem folders when actual findings or incidents warrant them.
