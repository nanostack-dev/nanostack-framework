# 0005: Concrete repositories per aggregate, explicit mapping

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Every repository in both services is declared twice, once as an interface and once as an implementation. Yet neither service has a code mock: the only "mock" in anchor is WireMock test data. "Go to definition" lands on the interface, not on the SQL.

Mapping goes through types such as echopoint's `FlowMapper struct{}`, which have no state and are provided through FX only to host methods. Repositories also log and return the error, which is the first of several log lines for one failure ([0008](0008-errors-are-wrapped-and-logged-once.md)).

## Decision

- **One repository per aggregate** (flow, version), written as a **concrete struct** with go-jet queries through [`pkg/db/transactor`](../../pkg/db/transactor/README.md).
- It returns domain types through **plain mapping functions**, and `functional.Option` for a row that may be absent.
- Repositories **don't log** and don't build HTTP-flavoured errors. They wrap errors with context, and the action decides what "absent" means.

```go
type FlowRepository struct{ db *sql.DB }

func (r *FlowRepository) FindByID(ctx context.Context, org string, id uuid.UUID) (functional.Option[flow.Flow], error) {
	stmt := table.Flows.SELECT(table.Flows.AllColumns).
		WHERE(table.Flows.ID.EQ(postgres.UUID(id)).
			AND(table.Flows.OrganizationID.EQ(postgres.String(org))))
	return transactor.QueryOptionalMap(ctx, r.db, stmt, flowToDomain)
}

// db_mapper.go
func flowToDomain(m model.Flows) flow.Flow { /* field by field */ }
```

```go
// lookups.go: "absent" becomes a 404 here, where the caller named the flow in the path.
func requireFlow(ctx context.Context, r *FlowRepository, org string, id uuid.UUID) (flow.Flow, error) {
	found, err := r.FindByID(ctx, org, id)
	if err != nil {
		return flow.Flow{}, fmt.Errorf("find flow %s: %w", id, err)
	}
	return found.ToResult(NewFlowNotFoundError(id.String())).Value()
}
```

An interface appears when a second implementation does, such as a cache in front of the repository, or when another feature consumes the repository ([0004](0004-one-action-per-write.md)).

## Alternatives

- **An interface per repository (status quo).** Keeps fakes possible, but tests run through HTTP and real Postgres ([0011](0011-http-tests-specify-table-tests-prove-rules.md)). Google calls an interface created "before a real need exists" the most common mistake ([best practices](https://google.github.io/styleguide/go/best-practices#avoid-unnecessary-interfaces)). The Go wiki says not to define one "for mocking" ([CodeReviewComments](https://go.dev/wiki/CodeReviewComments#interfaces)).
- **One repository per table.** Three Dots lists this as an anti-pattern: a write to one aggregate then spans several repositories and leaks the transaction ([transactions](https://threedots.tech/post/database-transactions-in-go/)).
- **sqlc**, which generates Go from SQL. An option for a new service. go-jet stays here because `transactor` and `jetx` build on it.
- **A generic `Repository[T]`.** It hides tenant scoping and column choices, and every real query ends up as an exception.
- **Reflection-based mappers.** Three Dots advises plain conversion functions instead ([anti-patterns](https://threedots.tech/post/common-anti-patterns-in-go-web-applications/)).

## Consequences

- About half the declarations disappear, and navigation lands on the real code.
- Swapping the storage of an aggregate later means introducing the interface then, as a mechanical change.
- [`pkg/db/transactor`](../../pkg/db/transactor/README.md) stays the only query entry point (decision log, 2026-07-27).
