// Package search holds one wrong search declaration per rule; nano gen repo
// reports every one of them in one run.
package search

import (
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

// nameFilter is an option in a variable: nano gen repo cannot read it.
var nameFilter = nanorepo.Filter(shop.Items.Name, nanorepo.Eq)

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
	nanorepo.Search[shopmodel.Items]("Twice", shop.Items)
	nanorepo.Search[shopmodel.Items]("Twice", shop.Items)
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

	// Shapes that nano gen repo cannot read.
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
}

func itemsWithTheirTenant() postgres.ReadableTable {
	return shop.Items.INNER_JOIN(shop.Tenants, shop.Tenants.ID.EQ(shop.Items.TenantID))
}

func itemsInTwoSteps() postgres.ReadableTable {
	from := shop.Items.INNER_JOIN(shop.Tenants, shop.Tenants.ID.EQ(shop.Items.TenantID))
	return from
}

func tenantsAlone() postgres.ReadableTable {
	return shop.Tenants
}

func ops() []nanorepo.Op { return []nanorepo.Op{nanorepo.Eq} }
