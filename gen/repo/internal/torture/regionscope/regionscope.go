// Package regionscope stands for a second context scope: the function that
// Postgres.ContextScopes registers for region_id in the torture fixture.
package regionscope

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
)

type regionKey struct{}

// With returns ctx carrying the region.
func With(ctx context.Context, region string) context.Context {
	return context.WithValue(ctx, regionKey{}, region)
}

// Filter keeps the rows of the region of ctx, or every row when ctx carries none.
func Filter(ctx context.Context, column postgres.ColumnString) postgres.BoolExpression {
	region, found := ctx.Value(regionKey{}).(string)
	if !found {
		return postgres.Bool(true)
	}
	return column.EQ(postgres.String(region))
}
