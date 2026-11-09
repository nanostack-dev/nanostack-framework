// Package readset holds one wrong read set per rule of design S5: a column
// or a function that reads an instance the search does not read (E5, E6),
// a call outside the allowlist (E7), a RIGHT, FULL or CROSS join (E28), a
// join that can repeat rows (E31), and an instance read twice. nano gen repo
// reports every one of them in one run.
package readset

import (
	"strings"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// parent is a prebuilt instance: nano gen repo reads functions, not values.
var parent = table.Tenants.AS("parent")

// expensiveCondition is a prebuilt expression.
var expensiveCondition = table.Items.Price.GT(postgres.Float(100))

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// E5: a column of an option on an instance the search does not read.
	nanorepo.Search[model.Items]("ScopeNotRead", table.Items,
		nanorepo.Scope(table.Tenants.ID),
		nanorepo.Sort(table.Items.Name),
	)
	nanorepo.Search[model.Items]("AliasNotRead", table.Items,
		nanorepo.From(itemsWithTheirTenant),
		nanorepo.SortKey(table.Tenants.AS("owner").Name).API("owner_name"),
	)
	nanorepo.Search[model.Tenants]("ExistsOtherTable", table.Tenants,
		nanorepo.FilterExists(table.Items, table.Tenants.ID, table.Items.Sku, nanorepo.In),
	)
	// E6: an expression function reads an instance the search does not
	// read; and one with an alias that the join does not give.
	nanorepo.Search[model.Items]("ExprNotRead", table.Items,
		nanorepo.Flag("NamedTenant", tenantNamed),
		nanorepo.FilterExpr("BossName", bossName, nanorepo.Eq),
	)
	nanorepo.Search[model.Items]("OnOtherAlias", table.Items, nanorepo.From(itemsOnOtherAlias))
	// E7: calls outside the allowlist, in From and in expression functions.
	nanorepo.Search[model.Items]("RawJoin", table.Items, nanorepo.From(rawJoin))
	nanorepo.Search[model.Items]("Custom", table.Items, nanorepo.Condition(custom))
	nanorepo.Search[model.Items]("TokenConversion", table.Items, nanorepo.Condition(tokenConversion))
	nanorepo.Search[model.Items]("FuncVariable", table.Items, nanorepo.FilterExpr("Lower", lowerByName, nanorepo.Eq))
	nanorepo.Search[model.Items]("CastVariable", table.Items, nanorepo.FullTextExpr(castByName))
	nanorepo.Search[model.Items]("ZoneVariable", table.Items, nanorepo.SortExpr("Day", dayInZone))
	nanorepo.Search[model.Items]("PackageTable", table.Items, nanorepo.From(itemsWithParent))
	nanorepo.Search[model.Items]("PackageExpr", table.Items, nanorepo.Condition(prebuilt))
	nanorepo.Search[model.Items]("OtherSchema", table.Items, nanorepo.From(itemsInOtherSchema))
	nanorepo.Search[model.Items]("Prefixed", table.Items, nanorepo.Condition(prefixed))
	nanorepo.Search[model.Items]("EnumValue", table.Items, nanorepo.Condition(enumValue))
	nanorepo.Search[model.Items]("ColumnByName", table.Items, nanorepo.SortExpr("Named", columnByName))
	nanorepo.Search[model.Items]("OtherPackage", table.Items, nanorepo.Condition(otherPackage))
	nanorepo.Search[model.Items]("OrderedSubquery", table.Items, nanorepo.SortExpr("First", orderedSubquery))
	nanorepo.Search[model.Items]("Closure", table.Items, nanorepo.Condition(closure))
	nanorepo.Search[model.Tenants]("TableSelect", table.Tenants, nanorepo.Flag("HasItems", tableSelect))
	nanorepo.Search[model.Items]("FuncLiteral", table.Items,
		nanorepo.Flag("Active", func() postgres.BoolExpression { return table.Items.Active.IS_TRUE() }),
	)
	// E28: RIGHT, FULL and CROSS joins.
	nanorepo.Search[model.Items]("CrossJoin", table.Items, nanorepo.From(itemsCrossTenants))
	nanorepo.Search[model.Items]("RightJoin", table.Items, nanorepo.From(itemsRightTenants))
	nanorepo.Search[model.Items]("FullJoin", table.Items, nanorepo.From(itemsFullTenants))
	// An instance read twice, by its alias or by a table name.
	nanorepo.Search[model.Items]("AliasTwice", table.Items, nanorepo.From(ownerTwice))
	nanorepo.Search[model.Items]("AliasIsATable", table.Items, nanorepo.From(tenantsNamedItems))
	// E31: a join that can return several rows per row before it, also
	// when the key is under an OR, or partly equated.
	nanorepo.Search[model.Tenants]("TenantItem", table.Tenants, nanorepo.From(tenantsAndTheirItems))
	nanorepo.Search[model.Items]("OrJoin", table.Items, nanorepo.From(itemsOrTenants))
	nanorepo.Search[model.Tenants]("HalfKey", table.Tenants, nanorepo.From(tenantsAndOneSku))
	// A From helper that nano gen repo cannot read.
	nanorepo.Search[model.Items]("HelperTwoSteps", table.Items, nanorepo.From(viaHelperInTwoSteps))
}

func itemsWithTheirTenant() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsOnOtherAlias() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.AS("t").ID.EQ(table.Items.TenantID))
}

func tenantNamed() postgres.BoolExpression { return table.Tenants.Name.EQ(postgres.String("acme")) }

func bossName() postgres.StringExpression { return table.Tenants.AS("boss").Name }

func rawJoin() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, postgres.RawBool("tenants.id = items.tenant_id"))
}

func custom() postgres.BoolExpression {
	return postgres.BoolExp(postgres.CustomExpression(table.Items.Active))
}

func tokenConversion() postgres.BoolExpression {
	var _ postgres.Serializer = postgres.Token("TRUE")
	return table.Items.Active.IS_TRUE()
}

func lowerByName() postgres.StringExpression {
	name := "lower"
	return postgres.StringExp(postgres.Func(name, table.Items.Name))
}

func castByName() postgres.StringExpression {
	return postgres.StringExp(postgres.CAST(table.Items.Sku).AS("text; DROP TABLE items"))
}

func dayInZone() postgres.TimestampExpression {
	zone := "UTC"
	return postgres.DATE_TRUNC(postgres.DAY, table.Items.CreatedAt, zone)
}

func itemsWithParent() postgres.ReadableTable {
	return table.Items.INNER_JOIN(parent, parent.ID.EQ(table.Items.TenantID))
}

func prebuilt() postgres.BoolExpression { return expensiveCondition }

func itemsInOtherSchema() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.FromSchema("archive"), table.Tenants.ID.EQ(table.Items.TenantID))
}

func prefixed() postgres.BoolExpression {
	return table.Items.WithPrefix("old_").Active.IS_TRUE()
}

func enumValue() postgres.BoolExpression {
	return table.Items.Status.EQ(postgres.NewEnumValue("ACTIVE"))
}

func columnByName() postgres.StringExpression { return postgres.StringColumn("name") }

func otherPackage() postgres.BoolExpression {
	return table.Items.Name.EQ(postgres.String(strings.ToLower("ACME")))
}

func orderedSubquery() postgres.StringExpression {
	return postgres.StringExp(postgres.SELECT(table.Tenants.Name).FROM(table.Tenants).
		ORDER_BY(table.Tenants.Name.ASC()).LIMIT(1))
}

func closure() postgres.BoolExpression {
	active := func() postgres.BoolExpression { return table.Items.Active.IS_TRUE() }
	return active()
}

func tableSelect() postgres.BoolExpression {
	return postgres.EXISTS(table.Items.SELECT(table.Items.ID).WHERE(table.Items.TenantID.EQ(table.Tenants.ID)))
}

func itemsCrossTenants() postgres.ReadableTable {
	return table.Items.CROSS_JOIN(table.Tenants)
}

func itemsRightTenants() postgres.ReadableTable {
	return table.Items.RIGHT_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsFullTenants() postgres.ReadableTable {
	return table.Items.FULL_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func ownerTwice() postgres.ReadableTable {
	return table.Items.
		INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID)).
		INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}

func tenantsNamedItems() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("items"), table.Tenants.AS("items").ID.EQ(table.Items.TenantID))
}

func tenantsAndTheirItems() postgres.ReadableTable {
	return table.Tenants.LEFT_JOIN(table.Items, table.Items.TenantID.EQ(table.Tenants.ID))
}

func itemsOrTenants() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants,
		table.Tenants.ID.EQ(table.Items.TenantID).OR(table.Tenants.Name.EQ(table.Items.Name)))
}

func tenantsAndOneSku() postgres.ReadableTable {
	return table.Tenants.INNER_JOIN(table.Items,
		table.Items.TenantID.EQ(table.Tenants.ID).AND(table.Items.Sku.NOT_EQ(table.Tenants.Name)))
}

func viaHelperInTwoSteps() postgres.ReadableTable { return twoSteps() }

func twoSteps() postgres.ReadableTable {
	from := table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
	return from
}
