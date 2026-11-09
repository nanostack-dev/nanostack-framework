// Package workspacescope stands for a product's context scope: the function
// that Postgres.ContextScopes registers for workspace_id in the fixtures.
package workspacescope

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// workspaceKey is the context key of the workspace.
type workspaceKey struct{}

// With returns ctx carrying the workspace id.
func With(ctx context.Context, workspace string) context.Context {
	return context.WithValue(ctx, workspaceKey{}, workspace)
}

// Filter keeps the rows of the workspace of ctx, or every row when ctx
// carries no workspace, as an organization-wide read does.
func Filter(ctx context.Context, column postgres.ColumnString) postgres.BoolExpression {
	workspace, found := ctx.Value(workspaceKey{}).(string)
	if !found {
		return postgres.Bool(true)
	}
	return column.EQ(postgres.String(workspace))
}
