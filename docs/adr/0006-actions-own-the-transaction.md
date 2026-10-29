# 0006: Actions own the transaction

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

[`transactor.InTx`](../../pkg/db/transactor/transactor.go) carries the transaction in `ctx`. It joins a transaction that is already there, and the outermost call owns the commit and the rollback (decision log, 2026-05-25). Today a transaction can start anywhere: in a service method, a helper or a cross-feature call. That makes the transaction boundary hard to find in review.

## Decision

- **Only an action's `Execute` opens a transaction**, with `InTx` ([0004](0004-one-action-per-write.md)).
- Repositories run on `transactor.Executor(ctx, db)` and never begin one.
- Handlers and `Queries` never open one.
- Code that must run inside a transaction checks `transactor.CurrentTx` and fails loudly without one.

```go
func (q *SearchReindexQueue) EnqueueFlows(ctx context.Context, org string, ids ...uuid.UUID) error {
	tx := transactor.CurrentTx(ctx)
	if tx == nil {
		return errors.New("enqueue flow reindex: transaction context missing")
	}
	return q.queue.InsertTx(ctx, tx, reindexJob{Org: org, FlowIDs: ids}) // illustrative queue call
}
```

## Alternatives

- **An `UpdateFn` closure in the repository.** Three Dots recommends this as the default, and says carrying the transaction in context "only masks the root issue" ([source](https://threedots.tech/post/database-transactions-in-go/)). The repository owns load, modify and save, and the rule runs in the closure:

  ```go
  func (r *FlowRepository) Update(ctx context.Context, org string, id uuid.UUID,
  	fn func(f *flow.Flow) error) (flow.Flow, error)

  restored, err := a.flows.Update(ctx, in.OrganizationID, in.FlowID, func(f *flow.Flow) error {
  	return f.RestoreFrom(version)
  })
  ```

  Excellent for one aggregate. It gets awkward when an action writes two aggregates and also enqueues a job, which is common here. It stays an option for aggregates with a state machine (OQ-2).

- **An explicit `tx` parameter.** The transaction is visible in every signature, but `*sql.Tx` then appears in every repository method and caller, and each method needs a second variant for use without a transaction.

  ```go
  func (r *FlowRepository) Update(ctx context.Context, tx *sql.Tx, f flow.Flow) (flow.Flow, error)
  ```

## Consequences

- The transaction boundary is the first thing a reviewer finds in `Execute`.
- Transactions compose across features: deleting a folder un-categorises its flows in one transaction, as echopoint does with `BulkUncategorizeByFolders`.
- The transaction is invisible in signatures. The single-opener rule and the `CurrentTx` guards make up for that.
- Row locks stay inline, as `gorules/transactor.go` already enforces.
