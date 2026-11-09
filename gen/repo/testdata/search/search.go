// Package search declares searches that nanostack gen repo reads without a
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
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
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
		nanorepo.GoOnly(nanorepo.FullTextExpr(tenantName)),
		nanorepo.Flag("Expensive", expensive),
		nanorepo.FilterExpr("TenantName", tenantName, nanorepo.Eq),
		nanorepo.Condition(expensive),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name", "tenant").NullsLast(),
		nanorepo.SortExpr("Total", total),
		nanorepo.SortHandWritten("popularity"),
		nanorepo.DefaultSortExpr("Total", nanorepo.Asc),
	)

	// A LEFT JOIN to one row: every column of tenants can be NULL in a
	// result row. The join keys on the primary key of tenants, so it adds
	// no row, and the primary key of items orders every row.
	nanorepo.Search[model.Items]("ItemOptionalTenant", table.Items,
		nanorepo.From(itemsAndTheirTenant),
		nanorepo.Filter(table.Tenants.Name, nanorepo.Present),
		nanorepo.Sort(table.Tenants.Name, table.Items.CreatedAt),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Asc),
	)

	// A joined table with an alias: the options name its columns through
	// AS, and -explain names them by the alias.
	nanorepo.Search[model.Items]("ItemAliasedTenant", table.Items,
		nanorepo.From(itemsWithAnAliasedTenant),
		nanorepo.Filter(table.Tenants.AS("owner").Name, nanorepo.Eq),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Tenants.AS("owner").Name).API("owner_name"),
		nanorepo.FilterExpr("OwnerName", ownerName, nanorepo.Eq),
	)

	// A From function that calls a helper, which nanostack gen repo follows; an
	// ON condition that equates the unique key tenants_name_key with a field
	// of the scope value, so the join adds one row; and expression functions
	// with Func, CAST, DATE_TRUNC with a constant time zone, and subqueries
	// whose instances have an alias. The join links no column of items to
	// tenants: the tenant check refuses this shape, and only the one-row
	// rule is shown here. The From function takes the scope value that nanostack
	// gen repo generates, ItemOfNamedTenantScope: the first load of the
	// package does not have it yet.
	nanorepo.Search[model.Items]("ItemOfNamedTenant", table.Items,
		nanorepo.From(itemsOfNamedTenant),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.Tenants.Name),
		nanorepo.SortExpr("Siblings", siblingCount),
		nanorepo.SortExpr("CreatedDay", createdDay),
		nanorepo.Flag("HasSiblings", hasSiblings),
		nanorepo.FilterExpr("LowerName", lowerName, nanorepo.Eq),
		nanorepo.FullTextExpr(skuText),
	)
}

func itemsOfNamedTenant(s ItemOfNamedTenantScope) postgres.ReadableTable { return joinNamedTenant(s) }

func joinNamedTenant(s ItemOfNamedTenantScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.Name.EQ(postgres.String(s.Name)))
}

func siblingCount() postgres.IntegerExpression {
	return postgres.IntExp(postgres.SELECT(postgres.COUNT(postgres.STAR)).
		FROM(table.Items.AS("sibling")).
		WHERE(table.Items.AS("sibling").TenantID.EQ(table.Items.TenantID)))
}

func createdDay() postgres.TimestampExpression {
	return postgres.DATE_TRUNC(postgres.DAY, table.Items.CreatedAt, "UTC")
}

func hasSiblings() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).
		FROM(table.Items.AS("sibling").
			INNER_JOIN(table.Tenants.AS("sibling_tenant"),
				table.Tenants.AS("sibling_tenant").ID.EQ(table.Items.AS("sibling").TenantID))).
		WHERE(table.Items.AS("sibling").TenantID.EQ(table.Items.TenantID).
			AND(table.Items.AS("sibling").ID.NOT_EQ(table.Items.ID))))
}

func lowerName() postgres.StringExpression {
	return postgres.StringExp(postgres.Func("lower", table.Items.Name))
}

func skuText() postgres.StringExpression {
	return postgres.StringExp(postgres.CAST(table.Items.Sku).AS("text"))
}

func ownerName() postgres.StringExpression { return table.Tenants.AS("owner").Name }

func itemsWithTheirTenant() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsAndTheirTenant() postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsWithAnAliasedTenant() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}

func tenantName() postgres.StringExpression { return table.Tenants.Name }

func expensive() postgres.BoolExpression { return table.Items.Price.GT(postgres.Float(100)) }

func total() postgres.FloatExpression { return table.Items.Price.MUL(postgres.Float(2)) }
