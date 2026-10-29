# 0011: HTTP tests specify, unit tests only for standalone logic

- Status: accepted
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Echopoint's component suite already has one HTTP test file per operation, such as `flow_publish_test.go` and `flow_version_restore_test.go`. Those tests send real HTTP requests to the service, which runs on a containerized Postgres with a real anchor container and WireMock for other third parties. Neither service has code mocks. The refactors in this guide change structure, not contracts, so this suite is what proves they are safe.

## Decision

We follow the [testing trophy](https://kentcdodds.com/blog/the-testing-trophy-and-testing-classifications): lint at the base ([0012](0012-conventions-are-enforced-by-lint.md)), mostly integration tests, and few unit tests.

1. **HTTP component tests are the specification.** Write one file per operation, as arrange/act/assert through the generated client, with the [contract-test harness](../../pkg/testkit/ct/README.md). Each test sends real HTTP requests to a service running on a containerized Postgres, a real anchor container and WireMock for other third parties.
2. **Crons and queue jobs are tested the same way.** The test starts the work over HTTP, the worker runs in the service, and the test waits until the effect shows through the API or WireMock. No test calls a worker or a cron function directly.
3. **Unit tests only for standalone logic.** A complex algorithm that uses neither the database nor HTTP gets a table test, usually in the domain package ([0003](0003-domain-types-carry-the-rules.md)). Cron parsing is an example.
4. **Repository integration tests only for SQL that is not trivial:** search, cursors, locking. They also run on a containerized Postgres.
5. **No mocks, and no unit tests for actions or handlers.** The HTTP tests cover those.

```go
// A cron or job: start it over HTTP, then wait for the visible effect (flow_schedule_test.go)
runResp, err := client.RunFlowScheduleNowWithResponse(ctx, scheduleID, nil)
require.Equal(t, http.StatusAccepted, runResp.StatusCode())
require.Eventually(t, func() bool {
	runs, err := client.ListFlowScheduleRunsWithResponse(ctx, scheduleID, params)
	return err == nil && len(runs.JSON200.Items) > 0 && runs.JSON200.Items[0].CompletedAt != nil
}, 30*time.Second, 250*time.Millisecond)
```

## Alternatives

- **Unit tests at every layer, with mocks (the test pyramid).** Fast, but they pin the internal structure, so every refactor in this guide would rewrite them. The value they would add is already covered by domain tests plus HTTP tests.
- **Calling workers directly.** Faster and deterministic, but it skips the enqueue in the action's transaction ([0007](0007-side-effects-leave-through-the-outbox.md)), where a lost job would come from.

## Consequences

- A refactor that leaves the HTTP suite untouched and green has preserved behaviour.
- A test that fails when code moves, but not when a feature breaks, tests implementation details and is removed.
- Mat Ryer: "I err on the side of end-to-end testing at this level" ([source](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/)). Three Dots runs component tests through the real ports and keeps unit tests for the domain ([test architecture](https://threedots.tech/post/microservices-test-architecture/)).
- HTTP tests are slower than unit tests, and job tests wait for their effect, so the suites must stay parallel and isolated per test, with short polling intervals.
