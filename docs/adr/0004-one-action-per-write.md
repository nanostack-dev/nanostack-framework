# 0004: One action per write, one Queries type per feature

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Both services put a feature's operations on one wide service struct.

- **Echopoint's `flowService`** holds 9 dependencies for about 30 methods in 1455 lines.
- **Anchor's `organizationInvitationService`** holds 7 dependencies.

Nothing shows which dependencies a given method touches. A one-line diff lands in a file that every operation shares. The same shape at scale reaches about 200 KB in Mattermost's `channel.go`, with 132 methods on `App` ([mattermost](https://github.com/mattermost/mattermost/tree/master/server/channels/app)), and about 82 KB in Grafana's `dashboard_service.go` ([grafana](https://github.com/grafana/grafana/tree/main/pkg/services/dashboards/service)).

## Decision

Every write operation is an **action**: one file, one struct named after the operation, its dependencies as fields, an `Input` type and a single `Execute`. All reads of a feature are methods on one `Queries` type. There are no exceptions for "thin" writes: when refactors are cheap, a uniform shape beats case-by-case judgement.

```go
// restore_flow_version.go
package flows

// RestoreFlowVersionInput names the version to copy back onto its flow.
type RestoreFlowVersionInput struct {
	OrganizationID string    `validate:"required,notblank"`
	FlowID         uuid.UUID `validate:"required"`
	VersionID      uuid.UUID `validate:"required"`
}

// RestoreFlowVersion copies a published version's definition and metadata back
// onto the flow, and re-indexes search in the same transaction (0007).
type RestoreFlowVersion struct {
	flows    *FlowRepository
	versions *VersionRepository
	reindex  SearchReindexer // narrow interface, implemented by resourcesearch
	tx       transactor.Transactor
}

func (a *RestoreFlowVersion) Execute(ctx context.Context, in RestoreFlowVersionInput) (flow.Flow, error) {
	if err := validate.ValidateStruct(in); err != nil {
		return flow.Flow{}, err
	}
	var restored flow.Flow
	err := a.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := requireFlow(ctx, a.flows, in.OrganizationID, in.FlowID)
		if err != nil {
			return err
		}
		version, err := requireVersion(ctx, a.versions, in.OrganizationID, in.FlowID, in.VersionID)
		if err != nil {
			return err
		}
		if err := current.RestoreFrom(version); err != nil {
			return err
		}
		if restored, err = a.flows.Update(ctx, current); err != nil {
			return fmt.Errorf("update flow: %w", err)
		}
		return a.reindex.EnqueueFlows(ctx, in.OrganizationID, restored.ID)
	})
	return restored, err
}
```

```go
// queries.go: reads protect no invariant, so one type keeps them together.
type Queries struct {
	flows    *FlowRepository
	versions *VersionRepository
}

func (q *Queries) Get(ctx context.Context, org string, id uuid.UUID) (flow.Flow, error) {
	return requireFlow(ctx, q.flows, org, id)
}
```

Rules:

- `Execute` validates its input on its first line.
- An action does not call another action of the same feature. A shared step becomes a function in `lookups.go` or in the domain.
- Another feature reaches an action or query through an interface **the consumer declares** ([Go wiki](https://go.dev/wiki/CodeReviewComments#interfaces)):

  ```go
  // in feature/executions
  type FlowVersionLookup interface {
  	GetVersion(ctx context.Context, org string, flowID, versionID uuid.UUID) (flow.Version, error)
  }
  // satisfied by *flows.Queries; flows knows nothing about executions
  ```

- `Input` types live beside the action. They describe one use case, not the entity (OQ-4).

## Alternatives

- **A wide service (status quo).** The simplest place to start, but a method's real dependencies are invisible and the file grows without limit.
- **A function per operation** (Temporal's [`signalworkflow.Invoke`](https://github.com/temporalio/temporal/blob/main/service/history/api/signalworkflow/api.go)). The lightest form. Temporal runs about 50 such per-operation packages behind a thin engine ([history/api](https://github.com/temporalio/temporal/tree/main/service/history/api)). With injected dependencies, though, the caller must hold all of them, so the facade turns back into the wide struct.

  ```go
  func Invoke(ctx context.Context, req *SignalRequest, shard ShardContext, checker ConsistencyChecker) (*SignalResponse, error)
  ```

- **CQRS handlers with generic decorators** ([Wild Workouts](https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example/blob/master/internal/trainings/app/command/cancel_training.go), [Basic CQRS](https://threedots.tech/post/basic-cqrs-in-go/)). The decorators add logging and metrics per command. Tracing middleware and the strict error handler already provide those, so the decorators would be machinery without a job.

  ```go
  type CancelTrainingHandler decorator.CommandHandler[CancelTraining]
  func (h cancelTrainingHandler) Handle(ctx context.Context, cmd CancelTraining) error
  ```

- **Inputs in the domain package** (anchor's `organizationinvitation/input_types.go`). Works, but the domain package then grows a type for every use case.

## Consequences

- Each operation shows its dependencies and blast radius. In the example, that is 4 dependencies, not 9. A diff to restore lands in one file of about 70 lines whose name says what changed.
- Each action costs about 15 lines of struct and input boilerplate.
- Twenty writes means twenty files, the same granularity as the HTTP tests ([0011](0011-http-tests-specify-table-tests-prove-rules.md)).
