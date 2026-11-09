// Package builder declares searches that read without a problem and that
// nano gen repo cannot write: each search shows one shape. -explain still
// shows them.
package builder

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// itemRow is a Row of its own. Its Weight is not a pointer, and the column
// is nullable.
type itemRow struct {
	model.Items
	Weight float32
}

// otherScope is not the scope value of any search.
type otherScope struct{ TenantID string }

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// A Row that is not the model of the table, and a sort on a nullable
	// column whose Row field is not a pointer.
	nanorepo.Search[itemRow]("OwnRow", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Items.Weight),
	)

	// No tenant condition: searchexec.DefineSpec refuses it. Unscoped is a
	// record, not a condition.
	nanorepo.Search[model.Items]("NoTenant", table.Items,
		nanorepo.Unscoped(table.Items.TenantID, "fixture"),
		nanorepo.Sort(table.Items.Name),
	)

	// No sort: searchexec.DefineSpec needs a key.
	nanorepo.Search[model.Items]("NoSort", table.Items,
		nanorepo.Scope(table.Items.TenantID),
	)

	// Operators that make two field types, a filter that hides a method of
	// the builder, two filters with one accessor, FilterExists, a Range on a
	// column that is not timestamptz, an Eq with no literal, NULL placement on a NOT NULL column, and a sort on a
	// column that has no key.
	nanorepo.Search[model.Items]("Shapes", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Sku, nanorepo.In, nanorepo.Present),
		nanorepo.FilterExpr("Limit", lowerName, nanorepo.Eq),
		nanorepo.Filter(table.Items.Name, nanorepo.Eq),
		nanorepo.FilterExpr("Name", lowerName, nanorepo.In),
		nanorepo.FilterExists(table.Tenants, table.Tenants.ID, table.Tenants.Name, nanorepo.In),
		nanorepo.Filter(table.Items.Stock, nanorepo.Range("min_stock", "max_stock")),
		nanorepo.Filter(table.Items.Price, nanorepo.Eq),
		nanorepo.SortKey(table.Items.Name).NullsFirst(),
		nanorepo.Sort(table.Items.Active),
	)

	// A From function that takes a value that is not the scope value of
	// the search.
	nanorepo.Search[model.Items]("WrongFrom", table.Items,
		nanorepo.From(itemsOf),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.Items.Sku),
		nanorepo.Sort(table.Items.Name),
	)
}

func _() {
	// Only SortHandWritten: the constant has no key, and DefineSpec needs one.
	nanorepo.Search[model.Items]("HandOnly", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortHandWritten("popularity"),
	)

	// One sort_by value given twice, by Sort and SortKey of one column.
	nanorepo.Search[model.Items]("TwiceKey", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Items.Name),
	)

	// One sort_by value given twice, by Sort and SortHandWritten.
	nanorepo.Search[model.Items]("TwiceHand", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortHandWritten("name"),
	)

	// Two API names whose constants have one Go name.
	nanorepo.Search[model.Items]("OneConst", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortKey(table.Items.Name).API("created-at"),
		nanorepo.Sort(table.Items.CreatedAt),
	)

	// Eq on a string column whose catalogue type is not text (inet): no
	// literal, as PostgreSQL would refuse a value that is not an address.
	nanorepo.Search[model.AuditEvents]("Client", table.AuditEvents,
		nanorepo.Scope(table.AuditEvents.ItemID),
		nanorepo.FilterAs[string](table.AuditEvents.Client, nanorepo.Eq),
		nanorepo.Sort(table.AuditEvents.At),
	)
}

func lowerName() postgres.StringExpression { return postgres.LOWER(table.Items.Name) }

func itemsOf(s otherScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Tenants.Name.EQ(postgres.String(s.TenantID))))
}
