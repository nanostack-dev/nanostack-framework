// Package search holds one wrong search declaration per rule; nanostack gen repo
// reports every one of them in one run.
package search

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	shopmodel "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	shop "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/search/db/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/search/db/table"
)

// sku is an int, and the model field of items.sku is a string.
type sku int

// mysqlItemsTable is a MySQL table.
type mysqlItemsTable struct {
	mysql.Table

	ID mysql.ColumnString
}

var mysqlItems = &mysqlItemsTable{}

// nameFilter is an option in a variable: nanostack gen repo cannot read it.
var nameFilter = nanorepo.Filter(shop.Items.Name, nanorepo.Eq)

// A direction and an API name in variables: nanostack gen repo cannot read them.
var (
	dir   = nanorepo.Desc
	until = "created_until"
)

func summarize(row shopmodel.Items) shopmodel.Items { return row }

func summarizePage(_ context.Context, rows []shopmodel.Items) ([]shopmodel.Items, error) {
	return rows, nil
}

func _() {
	nanorepo.Base[shopmodel.Tenants](shop.Tenants)

	// E2: Contains and FullText on columns that are not text.
	nanorepo.Search[shopmodel.Items]("NotText", shop.Items,
		nanorepo.Filter(shop.Items.CreatedAt, nanorepo.Contains),
		nanorepo.Filter(shop.Items.TenantID, nanorepo.Eq, nanorepo.Contains),
		nanorepo.Filter(shop.Items.Status, nanorepo.In, nanorepo.Contains),
		nanorepo.FullText(shop.Items.Name, shop.Items.Tags),
	)
	// E3: no primary key and no Tiebreak.
	nanorepo.Search[model.Counts]("Count", table.Counts)
	// E4: a Tiebreak that covers no unique key.
	nanorepo.Search[shopmodel.Items]("ShortTiebreak", shop.Items, nanorepo.Tiebreak(shop.Items.Sku))
	nanorepo.Search[model.Labels]("Label", table.Labels, nanorepo.Tiebreak(table.Labels.ExternalID, table.Labels.Code))
	nanorepo.Search[shopmodel.Items]("JoinedTiebreak", shop.Items,
		nanorepo.From(itemsWithTheirTenant),
		nanorepo.Tiebreak(shop.Tenants.ID, shop.Items.ID),
	)
	// E11: two searches with one name.
	nanorepo.Search[shopmodel.Items]("Twice", shop.Items, nanorepo.Scope(shop.Items.TenantID))
	nanorepo.Search[shopmodel.Items]("Twice", shop.Items, nanorepo.Scope(shop.Items.TenantID))
	// E13: FilterAs over another type than the model field.
	nanorepo.Search[shopmodel.Items]("WrongAs", shop.Items, nanorepo.FilterAs[sku](shop.Items.Sku, nanorepo.In))
	// E14: a default sort that is not a sort.
	nanorepo.Search[shopmodel.Items]("NoSuchSort", shop.Items,
		nanorepo.Sort(shop.Items.Name),
		nanorepo.DefaultSort(shop.Items.CreatedAt, nanorepo.Desc),
	)
	nanorepo.Search[shopmodel.Items]("NoSuchExpr", shop.Items, nanorepo.DefaultSortExpr("Total", nanorepo.Asc))
	// E16: a table of another dialect.
	nanorepo.Search[shopmodel.Items]("OnMySQL", mysqlItems)
	// E17: a table package from before ColumnTypes and ForeignKeys.
	nanorepo.Search[model.LegacyEvents]("Legacy", table.LegacyEvents,
		nanorepo.Sort(table.LegacyEvents.Action),
		nanorepo.DefaultSort(table.LegacyEvents.Action, nanorepo.Asc),
	)

	// Shapes that nanostack gen repo cannot read.
	nanorepo.Search[shopmodel.Items]("OptionVariable", shop.Items, nameFilter)
	nanorepo.Search[shopmodel.Items]("lowercase", shop.Items)
	nanorepo.Search[shopmodel.Items]("FromLiteral", shop.Items,
		nanorepo.From(func() postgres.ReadableTable { return shop.Items }))
	nanorepo.Search[shopmodel.Items]("FromTwoStatements", shop.Items, nanorepo.From(itemsInTwoSteps))
	nanorepo.Search[shopmodel.Items]("FromMissesTheTable", shop.Items, nanorepo.From(tenantsAlone))
	nanorepo.Search[shopmodel.Items]("TwoFroms", shop.Items,
		nanorepo.From(itemsWithTheirTenant),
		nanorepo.From(itemsWithTheirTenant),
	)
	nanorepo.Search[shopmodel.Items]("NotAnOp", shop.Items, nanorepo.Filter(shop.Items.Name, ops()...))

	// E2 on the child column of FilterExists.
	nanorepo.Search[shopmodel.Tenants]("ExistsNotText", shop.Tenants,
		nanorepo.FilterExists(shop.Items, shop.Items.TenantID, shop.Items.CreatedAt, nanorepo.Contains),
	)
	// E11 is reported also when the first search of the name has a problem.
	nanorepo.Search[shopmodel.Items]("TwiceBroken", shop.Items, nanorepo.Filter(shop.Items.CreatedAt, nanorepo.Contains))
	nanorepo.Search[shopmodel.Items]("TwiceBroken", shop.Items)
	// E4: an empty Tiebreak, and a column named twice.
	nanorepo.Search[shopmodel.Items]("EmptyTiebreak", shop.Items, nanorepo.Tiebreak())
	nanorepo.Search[shopmodel.Items]("RepeatedTiebreak", shop.Items, nanorepo.Tiebreak(shop.Items.ID, shop.Items.ID))
	// The table of the search on the nullable side of a LEFT JOIN.
	nanorepo.Search[shopmodel.Items]("NullableRoot", shop.Items, nanorepo.From(tenantsAndTheirItems))
	// The table of the search with an alias.
	nanorepo.Search[shopmodel.Items]("AliasedRoot", shop.Items, nanorepo.From(aliasedItems))
	// E14 names the table of each column.
	nanorepo.Search[shopmodel.Items]("DefaultSortOtherTable", shop.Items,
		nanorepo.From(itemsWithTheirTenant),
		nanorepo.Sort(shop.Items.Name),
		nanorepo.DefaultSort(shop.Tenants.Name, nanorepo.Asc),
	)
	// Options that nanostack gen repo cannot read, or that are out of range.
	nanorepo.Search[shopmodel.Items]("VariableDirection", shop.Items,
		nanorepo.Sort(shop.Items.CreatedAt),
		nanorepo.DefaultSort(shop.Items.CreatedAt, dir),
	)
	nanorepo.Search[shopmodel.Items]("VariableRangeName", shop.Items,
		nanorepo.Filter(shop.Items.CreatedAt, nanorepo.Range("created_since", until)),
	)
	nanorepo.Search[shopmodel.Items]("GoOnlySort", shop.Items, nanorepo.GoOnly(nanorepo.Sort(shop.Items.Name)))
	nanorepo.Search[shopmodel.Items]("ZeroLimit", shop.Items, nanorepo.DefaultLimit(0))
	nanorepo.Search[shopmodel.Items]("HugeLimit", shop.Items, nanorepo.DefaultLimit(5000))
	nanorepo.Search[shopmodel.Items]("MapAndMapPage", shop.Items,
		nanorepo.Map(summarize),
		nanorepo.MapPage(summarizePage),
	)
	nanorepo.Search[shopmodel.Items]("BlankReason", shop.Items,
		nanorepo.Unscoped(shop.Items.TenantID, " "),
		nanorepo.IgnoresFullText(""),
	)
	nanorepo.Search[shopmodel.Items]("ForgedReason", shop.Items,
		nanorepo.Unscoped(shop.Items.TenantID, "fixture\n  scope      Scope items.tenant_id"),
	)
	// E17: ColumnTypes misses a column; UniqueKeys is missing; the primary
	// key of the model is not in UniqueKeys.
	nanorepo.Search[model.Untyped]("Untyped", table.Untyped)
	nanorepo.Search[model.Keyless]("Keyless", table.Keyless)
	nanorepo.Search[model.Rekeyed]("Rekeyed", table.Rekeyed)
}

func itemsWithTheirTenant() postgres.ReadableTable {
	return shop.Items.INNER_JOIN(shop.Tenants, shop.Tenants.ID.EQ(shop.Items.TenantID))
}

// tenantsAndTheirItems joins at most one item per tenant (the key
// items_tenant_sku_key), so the problem is the nullable side alone.
func tenantsAndTheirItems() postgres.ReadableTable {
	return shop.Tenants.LEFT_JOIN(shop.Items,
		shop.Items.TenantID.EQ(shop.Tenants.ID).AND(shop.Items.Sku.EQ(shop.Tenants.Name)))
}

func aliasedItems() postgres.ReadableTable {
	return shop.Items.AS("i").INNER_JOIN(shop.Tenants, shop.Tenants.ID.EQ(shop.Items.TenantID))
}

func itemsInTwoSteps() postgres.ReadableTable {
	from := shop.Items.INNER_JOIN(shop.Tenants, shop.Tenants.ID.EQ(shop.Items.TenantID))
	return from
}

func tenantsAlone() postgres.ReadableTable {
	return shop.Tenants
}

func ops() []nanorepo.Op { return []nanorepo.Op{nanorepo.Eq} }
