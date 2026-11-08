# 0014: One repository with nested modules

- Status: proposed
- Date: 2026-11-07

## Context

The framework is spread over four library repositories: `nanostack-framework`, `pgkit`, `cron` and `echopoint-kit`. They pin each other's versions. A change that crosses them takes one release per repository, in order, and each consumer bump repeats the chain.

`nanostack-framework` itself is one module. Every consumer resolves the metadata of every dependency in it (testcontainers, PostgreSQL drivers, go-redis, OpenTelemetry), even to use `pkg/functional`, and `pkg/functional` cannot reach v1 while it shares a version with fast-moving packages.

Products (`echopoint`, `anchor` and the others) release on their own schedules and run product-specific CI gates, such as the post-deploy flow gate in the workspace guide.

## Decision

The four library repositories become one repository, `github.com/nanostack-dev/nanostack`, with about a dozen nested modules. This is the OpenTelemetry-Go and `golang.org/x/tools` layout.

- Each module has its own `go.mod`. Import paths are `github.com/nanostack-dev/nanostack/<dir>` and tags are `<dir>/vX.Y.Z`. Every module starts at v0. `functional` and `cron` are the first candidates for v1.
- `go.work` is for local development only and is not committed. CI builds and tests each module with `GOWORK=off`, so the versions in each `go.mod` are what is tested.
- A release module has no `replace` directive. The release script (`nano release`) refuses to tag one, tags changed modules leaves first, bumps the intra-repository requirements of dependents and tags them.
- Generators and tools are pinned in a `tools/` module.
- Every module follows one contract: README, AGENTS.md, CONTEXT.md, `doc.go`, an example test, one root lint configuration, and the shared analyzers. `nano doctor` and CI fail a module that breaks it.
- History is imported with `git subtree add` (or `git filter-repo` into a subdirectory) so that blame survives. The old repositories are archived with a README pointer and keep their tags, so existing `go.mod` pins still resolve.
- Products stay in their own repositories.

## Alternatives

- **One repository, one module.** The simplest release: one tag and no workspace. Rejected because every consumer would still resolve every dependency, and `functional` could not reach v1 on its own.
- **Keep separate repositories.** No migration. Rejected because the cross-pinned versions and chained releases are the problem this record solves.
- **Monorepo with a publishing bot (the Kubernetes staging model).** Separate import paths per published repository. Rejected as heavy infrastructure for a one-developer team, with no benefit nested modules lack.
- **Include the products.** One repository for everything. Rejected because products release on their own schedules, and their CI gates and deploys are product-specific.

## Consequences

- A cross-library change is one pull request and one CI run, and modules version independently.
- The framework maintains a tagging script and a check that rejects `replace` directives at tag time. Renovate's gomod manager updates several `go.mod` files, with intra-repository bumps grouped.
- Products change import paths once. Generic type aliases in the old paths can ease the move where they help.
- Private modules need `GOPRIVATE=github.com/nanostack-dev/*` and a token-backed git rule in CI. Making the repository public later needs no layout change.
- Compatibility checks stay consumer builds per module, as the 2026-11-07 "Check API Compatibility With Consumer Builds" entry in the [decision log](../decision-log.md) describes, until a module reaches v1.
