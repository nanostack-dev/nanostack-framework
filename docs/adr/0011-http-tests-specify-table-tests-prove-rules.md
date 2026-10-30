# 0011: HTTP tests specify, unit tests only for standalone logic

- Status: accepted
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Echopoint's component suite already has one HTTP test file per operation, such as `flow_publish_test.go` and `flow_version_restore_test.go`. Those tests send real HTTP requests to the service, which runs on a containerized Postgres with a real anchor container and WireMock for other third parties. Its service suite already calls some jobs directly, such as `ProcessUsageSyncJob`. Neither service has code mocks. The refactors in this guide change structure, not contracts, so this suite is what proves they are safe.

## Decision

We follow the [testing trophy](https://kentcdodds.com/blog/the-testing-trophy-and-testing-classifications): lint at the base ([0012](0012-conventions-are-enforced-by-lint.md)), mostly integration tests, and few unit tests.

1. **HTTP component tests are the specification.** Write one file per operation, as arrange/act/assert through the generated client, with the [contract-test harness](../../pkg/testkit/ct/README.md). Each test sends real HTTP requests to a service running on a containerized Postgres, a real anchor container and WireMock for other third parties.
2. **Crons and queue jobs: call the method they run, in a service test.** A job or a cron always runs one method. A service test builds only what that method needs, calls it and checks the result, with no HTTP request into the service and no waiting. It is still an integration test: the same containerized Postgres, the real anchor container and WireMock, reached over real HTTP. Only the entry point changes. An HTTP test that starts the work and waits for the effect is kept for when the trigger is itself the behaviour, such as `RunFlowScheduleNow`.
3. **Unit tests only for standalone logic.** A complex algorithm that uses neither the database nor HTTP gets a table test, usually in the domain package ([0003](0003-domain-types-carry-the-rules.md)). Cron parsing is an example.
4. **Repository integration tests only for SQL that is not trivial:** search, cursors, locking. They also run on a containerized Postgres.
5. **No mocks, and no unit tests for actions or handlers.** The HTTP tests cover those. No mock library or call expectations; a service test may pass a plain function for a collaborator it does not test, like the usage count below.

```go
// A job: build what it needs, call its method, check the result (cmd/it/service/license_usage_sync_test.go)
err := licenses.NewLicenseUsageSyncProcessor(registry, anchorClient(t)).
	ProcessUsageSyncJob(ctx, licenses.UsageSyncPayload{OrganizationID: org.ID, Field: "max_flows"}, queue.Job{})
require.NoError(t, err)
license, _ := svcCtx.Anchor.FindLicense(t, org.ID) // real Anchor container
assert.InDelta(t, 3, *license.Usage["max_flows"].Usage, 0)
```

## Alternatives

- **Unit tests at every layer, with mocks (the test pyramid).** Fast, but they pin the internal structure, so every refactor in this guide would rewrite them. The value they would add is already covered by domain tests plus HTTP tests.
- **Only HTTP tests for jobs and crons.** It also covers the enqueue, but every test waits and polls, and edge cases such as retries or a deleted organization are hard to reach.

## Consequences

- A refactor that leaves the HTTP suite untouched and green has preserved behaviour.
- A test that fails when code moves, but not when a feature breaks, tests implementation details and is removed.
- Mat Ryer: "I err on the side of end-to-end testing at this level" ([source](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/)). Three Dots runs component tests through the real ports and keeps unit tests for the domain ([test architecture](https://threedots.tech/post/microservices-test-architecture/)).
- A service test starts after the enqueue in the action's transaction ([0007](0007-side-effects-leave-through-the-outbox.md)). A missing enqueue is caught only by an HTTP test that looks for the job's effect, so keep one where the effect is visible.
- HTTP tests are slower than unit tests, so the suites must stay parallel and isolated per test.
