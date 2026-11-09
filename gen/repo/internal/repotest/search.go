package repotest

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// This file declares only a search. nano gen repo writes its builder into
// search_repo.gen.go, and the integration tests run it on PostgreSQL: every
// field type, and a keyset walk on a key of each kind of column.
func _() {
	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.ID, nanorepo.In),
		nanorepo.Filter(table.Items.Status, nanorepo.Eq, nanorepo.In),
		nanorepo.Filter(table.Items.Name, nanorepo.Contains, nanorepo.ContainsAny),
		nanorepo.Filter(table.Items.Stock, nanorepo.Eq),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("created_since", "created_until")),
		nanorepo.Filter(table.Items.ExternalID, nanorepo.Present),
		nanorepo.FilterExpr("UpperSku", upperSku, nanorepo.InFold),
		nanorepo.Flag("Expensive", expensive),
		nanorepo.FullText(table.Items.Name, table.Items.Sku),
		nanorepo.Sort(table.Items.Name, table.Items.CreatedAt, table.Items.Stock, table.Items.Sold),
		nanorepo.Sort(table.Items.Price, table.Items.Status, table.Items.LocalAt),
		nanorepo.SortKey(table.Items.Weight).API("weight", "mass").NullsLast(),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
	)
}

// upperSku is the SKU in capitals, for a FilterExpr.
func upperSku() postgres.StringExpression { return postgres.UPPER(table.Items.Sku) }

// expensive keeps the items that cost more than 10.
func expensive() postgres.BoolExpression { return table.Items.Price.GT(postgres.Float(10)) }
