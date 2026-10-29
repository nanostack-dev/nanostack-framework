# 0003: Domain types carry the rules

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

About 170 lines of echopoint's `flows/service.go` touch no dependency at all: `validateEdgeEndpoints`, `rejectDottedNodeIDs`, `removedFlowNodeIDs` and others. They sit beside methods that do I/O, so they are hard to find and are only tested through HTTP.

Anchor's `license/rules` package shows the other way. Its doc comment says it is "pure logic … table-tested directly rather than through a hundred HTTP round-trips".

## Decision

- Rules that need no I/O are methods or functions in `internal/domain/<entity>`.
- Constructors enforce creation invariants. Methods perform state transitions and return a named error for an illegal move.
- Fields stay exported, so mappers stay trivial.
- An aggregate with a real state machine, such as an execution's status, switches to private fields.

The package imports only framework `pkg/` primitives such as `fault` and `functional`. It takes no `context` and does no I/O.

```go
package flow

// Validate reports the first structural defect of a definition.
// Pure: the module-reference check that needs the database stays in the action.
func (d Definition) Validate() error {
	if err := rejectDottedNodeIDs(d.Nodes); err != nil {
		return err
	}
	return validateEdgeEndpoints(d.Nodes, d.Edges)
}

// RestoreFrom copies a published version back onto its own flow.
func (f *Flow) RestoreFrom(v Version) error {
	if v.FlowID != f.ID {
		return ErrVersionOfAnotherFlow
	}
	f.Definition, f.Metadata = v.Definition, v.Metadata
	return nil
}
```

```go
func TestDefinitionValidate(t *testing.T) {
	tests := []struct {
		name string
		def  flow.Definition
		want error
	}{
		{"dotted node id", defWithNode("a.b"), flow.ErrDottedNodeID},
		{"edge to unknown node", defWithEdge("a", "zz"), flow.ErrUnknownEdgeEndpoint},
		{"valid", defWithNodes("a", "b"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, tt.def.Validate(), tt.want)
		})
	}
}
```

## Alternatives

- **Validator tags only** (`pkg/validate`, anchor's `input_types.go`). Right for shape: required fields, email format, `oneof`. For business rules, though, tags hide the rule in a string the compiler cannot check, which Three Dots lists as an anti-pattern ([anti-patterns](https://threedots.tech/post/common-anti-patterns-in-go-web-applications/)). Tags stay on `Input` types ([0004](0004-one-action-per-write.md)), and rules stay out of them.
- **Private fields with getters everywhere** ([DDD Lite](https://threedots.tech/post/ddd-lite-in-go-introduction/)). This is the strongest guarantee, since nothing outside the package can build an invalid value. The cost is getters plus a separate constructor for mappers. Use it only where an invalid state is a real risk:

  ```go
  type Execution struct {
  	id     uuid.UUID
  	status Status
  }

  func (e *Execution) Complete(at time.Time) error {
  	if e.status != StatusRunning {
  		return ErrNotRunning
  	}
  	e.status = StatusSucceeded
  	return nil
  }
  ```

## Consequences

- Rules can be read and tested without Postgres ([0011](0011-http-tests-specify-table-tests-prove-rules.md)).
- With exported fields, an invariant holds only if code goes through the methods, so review has to check that.
- depguard keeps the package pure ([0012](0012-conventions-are-enforced-by-lint.md)).
