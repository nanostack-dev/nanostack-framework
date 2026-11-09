// Package search declares searches that nano gen repo reads without a
// problem; its -explain shows what it found.
package search

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// sku is a named type over the model field of items.sku.
type sku string

// itemSummary is the domain type of a search row.
type itemSummary struct{ ID string }

func summarize(row model.Items) itemSummary { return itemSummary{ID: row.ID} }

func summarizePage(_ context.Context, rows []model.Items) ([]itemSummary, error) {
	out := make([]itemSummary, len(rows))
	for i, row := range rows {
		out[i] = summarize(row)
	}
	return out, nil
}

func tenantFilter(context.Context, postgres.ColumnString) postgres.BoolExpression {
	return postgres.Bool(true)
}

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// Shaped like echopoint B10: the order is the sort, then sku, then id.
	// Tiebreak(Sku, ID) covers the primary key items_pkey (id).
	nanorepo.Search[model.Items]("ItemBySku", table.Items,
		nanorepo.Map(summarize),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Status, nanorepo.Eq, nanorepo.In),
		nanorepo.FilterAs[sku](table.Items.Sku, nanorepo.In.API("skus")),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("created_since", "created_until").HalfOpen()),
		nanorepo.GoOnly(nanorepo.Filter(table.Items.DeletedAt, nanorepo.Present)),
		nanorepo.Sort(table.Items.Name, table.Items.CreatedAt),
		nanorepo.Tiebreak(table.Items.Sku, table.Items.ID),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
		nanorepo.DefaultLimit(50),
	)

	// A Tiebreak that covers items_tenant_sku_key and items_pkey names the
	// primary key, whatever the order of the key names.
	nanorepo.Search[model.Items]("ItemByTenantSku", table.Items,
		nanorepo.Tiebreak(table.Items.TenantID, table.Items.Sku, table.Items.ID),
	)

	// An INNER JOIN, and the primary key as the default tiebreak.
	nanorepo.Search[model.Items]("ItemWithTenant", table.Items,
		nanorepo.MapPage(summarizePage),
		nanorepo.From(itemsWithTheirTenant),
		nanorepo.Scope(table.Items.TenantID, table.Tenants.ID),
		nanorepo.Unscoped(table.Items.TenantID, "fixture"),
		nanorepo.ScopeFunc(tenantFilter, table.Tenants.Name),
		nanorepo.Filter(table.Items.Name, nanorepo.Contains),
		nanorepo.FullText(table.Items.Name, table.Tenants.Name),
		nanorepo.FullTextExpr(tenantName),
		nanorepo.Flag("Expensive", expensive),
		nanorepo.FilterExpr("TenantName", tenantName, nanorepo.Eq),
		nanorepo.Condition(expensive),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name", "tenant").NullsLast(),
		nanorepo.SortExpr("Total", total),
		nanorepo.SortHandWritten("popularity"),
		nanorepo.DefaultSortExpr("Total", nanorepo.Asc),
	)

	// A LEFT JOIN: every column of items can be NULL in a result row.
	nanorepo.Search[model.Tenants]("TenantItem", table.Tenants,
		nanorepo.From(tenantsAndTheirItems),
		nanorepo.Filter(table.Items.Name, nanorepo.Present),
		nanorepo.Sort(table.Items.CreatedAt, table.Tenants.Name),
		nanorepo.DefaultSort(table.Tenants.Name, nanorepo.Asc),
	)
}

func itemsWithTheirTenant() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func tenantsAndTheirItems() postgres.ReadableTable {
	return table.Tenants.LEFT_JOIN(table.Items, table.Items.TenantID.EQ(table.Tenants.ID))
}

func tenantName() postgres.StringExpression { return table.Tenants.Name }

func expensive() postgres.BoolExpression { return table.Items.Price.GT(postgres.Float(100)) }

func total() postgres.FloatExpression { return table.Items.Price.MUL(postgres.Float(2)) }
