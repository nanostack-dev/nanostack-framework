# 0011: HTTP tests specify, table tests prove rules

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Echopoint's component suite already has one HTTP test file per operation, such as `flow_publish_test.go` and `flow_version_restore_test.go`. Those tests run against real Postgres, a real anchor container and WireMock for other third parties. Neither service has code mocks. The refactors in this guide change structure, not contracts, so this suite is what proves they are safe.

## Decision

1. **HTTP component tests are the specification.** Write one file per operation, as arrange/act/assert through the generated client, with the [contract-test harness](../../pkg/testkit/ct/README.md).
2. **Table tests cover domain rules**, in the domain package ([0003](0003-domain-types-carry-the-rules.md)).
3. **Repository integration tests only for SQL that is not trivial:** search, cursors, locking.
4. **No mocks, and no unit tests for actions or handlers.** The HTTP tests cover those.

## Alternatives

- **Unit tests at every layer, with mocks.** Fast, but they pin the internal structure, so every refactor in this guide would rewrite them. The value they would add is already covered by domain tests plus HTTP tests.

## Consequences

- A refactor that leaves the HTTP suite untouched and green has preserved behaviour.
- Mat Ryer: "I err on the side of end-to-end testing at this level" ([source](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/)). Three Dots runs component tests through the real ports and keeps unit tests for the domain ([test architecture](https://threedots.tech/post/microservices-test-architecture/)).
- HTTP tests are slower than unit tests, so the suites must stay parallel and isolated per test.
