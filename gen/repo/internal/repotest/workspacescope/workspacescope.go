// Package workspacescope stands for a product's context scope: the function
// that Postgres.ContextScopes registers for workspace_id in the fixtures.
package workspacescope

import (
	"context"
	"errors"

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

// ErrNoWorkspace refuses a new row written outside any workspace.
var ErrNoWorkspace = errors.New("an owned row needs a workspace: the context carries none")

// Stamp is the workspace a new row belongs to, the one of ctx. A write
// without a workspace is an error: unlike a read, it cannot fall back to
// the whole tenant.
func Stamp(ctx context.Context) (string, error) {
	workspace, found := ctx.Value(workspaceKey{}).(string)
	if !found {
		return "", ErrNoWorkspace
	}
	return workspace, nil
}

// Present reports whether ctx carries a workspace: the Present function of
// ContextScope for workspace_id, so that a derived delete or write on a bare
// context fails with nanorepo.ErrNoContextScope instead of reaching every
// workspace.
func Present(ctx context.Context) bool {
	_, found := ctx.Value(workspaceKey{}).(string)
	return found
}
