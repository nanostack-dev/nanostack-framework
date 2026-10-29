# 0009: Handlers only translate

- Status: proposed
- Date: 2026-10-29
- Part of: [feature architecture](../feature-architecture.md)

## Context

Both services generate an oapi-codegen **strict server** from their OpenAPI contract: a typed request object in, a typed response object out ([oapi-codegen](https://github.com/oapi-codegen/oapi-codegen#strict-server)). The framework renders their errors (decision log, 2026-05-23).

- **Echopoint** embeds one `Handler` per feature into `EchopointAPI`, so each feature owns its HTTP code.
- **Anchor** implements the whole interface on one `AnchorAPI` struct. That is why its ADR-0007 had to keep handlers out of its licensing slice.

Handlers in both services also log errors, which [0008](0008-errors-are-wrapped-and-logged-once.md) removes.

## Decision

- OpenAPI is the contract.
- Each feature has one `Handler` implementing its operations, and `api.Server` embeds every feature's handler.
- A handler reads the tenant from `ctx`, builds the `Input`, calls the action or query, and maps the result to a response.
- A handler doesn't log and doesn't branch on business logic.
- Enums use `x-go-type`, so the generated binding rejects unknown values with a 400 before the handler runs.

```go
func (h *Handler) RestoreFlowVersion(ctx context.Context, r apigen.RestoreFlowVersionRequestObject) (apigen.RestoreFlowVersionResponseObject, error) {
	restored, err := h.restore.Execute(ctx, RestoreFlowVersionInput{
		OrganizationID: contextkeys.OrganizationID(ctx),
		FlowID:         r.Id,
		VersionID:      r.VersionId,
	})
	if err != nil {
		return nil, err // the strict error handler maps the fault and logs once
	}
	return apigen.RestoreFlowVersion200JSONResponse(flowToResponse(restored)), nil
}
```

```go
// internal/api/server.go
type Server struct {
	*flows.Handler
	*executions.Handler
	// one embedded handler per feature
}

var _ apigen.StrictServerInterface = (*Server)(nil)
```

## Alternatives

- **A single API struct implementing everything** (anchor today). Feature code cannot own its handlers.
- **Per-tag generated interfaces.** Same result as embedding, but it needs custom generation configuration per service.
- **Hand-written routes** ([Mat Ryer](https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/): `routes.go` plus handler maker functions). Explicit and pleasant, but it gives up the generated clients and the contract-first tests.

## Consequences

- Handlers become short and uniform. A handler diff is only ever a contract change.
- Anchor needs per-feature handler types before its features can own their HTTP code. The field names of embedded handlers must not collide.
