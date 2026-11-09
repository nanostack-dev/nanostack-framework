// Package computed declares SortExpr keys that nano gen repo refuses
// (design S9, E15 and E8): a nullable aggregate in a non-pointer field, a
// subquery that reads a column instead of an aggregate, a field the Row
// lacks, and a field of another type.
package computed

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

type eventRow struct {
	model.Items
	LastEvent  int64
	FirstEvent string
	Count      string
	Fine       *int64
}

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[eventRow]("ComputedKeys", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortExpr("LastEvent", lastEventID),
		nanorepo.SortExpr("FirstEvent", firstEventItem),
		nanorepo.SortExpr("Missing", eventCount),
		nanorepo.SortExpr("Count", eventCount),
		nanorepo.SortExpr("Fine", lastEventID),
	)
}

func lastEventID() postgres.IntegerExpression {
	return postgres.IntExp(postgres.SELECT(postgres.MAXi(table.ItemEvents.ID)).FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.ID)))
}

func firstEventItem() postgres.StringExpression {
	return postgres.StringExp(postgres.SELECT(table.ItemEvents.ItemID).FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.ID)))
}

func eventCount() postgres.IntegerExpression {
	return postgres.IntExp(postgres.SELECT(postgres.COUNT(table.ItemEvents.ID)).FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.ID)))
}
