// Package readset holds one wrong read set per rule of design S5: a column
// or a function that reads an instance the search does not read (E5, E6),
// a call outside the allowlist (E7), a RIGHT, FULL or CROSS join (E28), a
// join that can repeat rows (E31), and an instance read twice. nanostack gen repo
// reports every one of them in one run.
package readset

import (
	"strings"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// parent is a prebuilt instance: nanostack gen repo reads functions, not values.
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
	// A From helper that nanostack gen repo cannot read.
	nanorepo.Search[model.Items]("HelperTwoSteps", table.Items, nanorepo.From(viaHelperInTwoSteps))

	// E7: a prebuilt DB value in a package variable whose type is not a DB
	// type: a struct field, an interface, a map and a named interface type.
	nanorepo.Search[model.Items]("StructVar", table.Items, nanorepo.Condition(structVar))
	nanorepo.Search[model.Items]("AnyVar", table.Items, nanorepo.Condition(anyVar))
	nanorepo.Search[model.Items]("MapVar", table.Items, nanorepo.Condition(mapVar))
	nanorepo.Search[model.Items]("NamedVar", table.Items, nanorepo.Condition(namedVar))
	// E7: a scope value that can carry a condition into the join.
	nanorepo.Search[model.Items]("ScopeWithCondition", table.Items, nanorepo.From(itemsWithExtra))
	nanorepo.Search[model.Items]("ScopeIsCondition", table.Items, nanorepo.From(itemsWithCondition))
	// E7: a subquery FROM that is not a table variable: a local variable, a
	// helper call, and a conversion. A refused join of a helper call.
	nanorepo.Search[model.Items]("SubqueryLocal", table.Items, nanorepo.Flag("Acme", subqueryLocal))
	nanorepo.Search[model.Items]("SubqueryHelper", table.Items, nanorepo.Flag("Any", subqueryHelper))
	nanorepo.Search[model.Items]("SubqueryConversion", table.Items, nanorepo.Flag("Some", subqueryConversion))
	nanorepo.Search[model.Items]("CrossJoinHelper", table.Items, nanorepo.From(itemsCrossHelper))
	// E7: a method value.
	nanorepo.Search[model.Items]("MethodValue", table.Items, nanorepo.Condition(methodValue))
	// E7: a DB function that builds a statement, not an expression.
	nanorepo.Search[model.Items]("Statement", table.Items, nanorepo.Condition(statement))
	nanorepo.Search[model.Items]("NoExpression", table.Items, nanorepo.Condition(noExpression))
	// E7: an alias that SQL could read as another name.
	nanorepo.Search[model.Items]("LongAlias", table.Items,
		nanorepo.Sort(table.Items.AS("an_alias_that_is_longer_than_the_sixty_three_bytes_that_postgresql_keeps").Name),
	)
	nanorepo.Search[model.Items]("UpperAlias", table.Items, nanorepo.From(itemsWithUpperAlias))
	// E6: a column of a subquery instance read outside that subquery.
	nanorepo.Search[model.Items]("OutsideSubquery", table.Items, nanorepo.Flag("Shared", outsideSubquery))
	// E6: an ON condition that reads an instance joined after it.
	nanorepo.Search[model.Items]("OnLaterJoin", table.Items, nanorepo.From(onLaterJoin))
	// E5: FilterExists names a column of an alias of its child table.
	nanorepo.Search[model.Tenants]("ExistsAlias", table.Tenants,
		nanorepo.FilterExists(table.Items, table.Items.AS("x").TenantID, table.Items.AS("x").Sku, nanorepo.In),
	)
	// E4: a Tiebreak on another instance of the table of the search.
	nanorepo.Search[model.Items]("TiebreakOther", table.Items,
		nanorepo.From(itemsAndOther),
		nanorepo.Tiebreak(table.Items.AS("other").ID),
	)
	nanorepo.Search[model.Items]("TiebreakBoth", table.Items,
		nanorepo.From(itemsAndOther),
		nanorepo.Tiebreak(table.Items.ID, table.Items.AS("other").ID),
	)
	// E14: a DefaultSort on another instance than the sort.
	nanorepo.Search[model.Tenants]("DefaultOtherInstance", table.Tenants,
		nanorepo.From(tenantsAndOwner),
		nanorepo.Sort(table.Tenants.Name),
		nanorepo.DefaultSort(table.Tenants.AS("owner").Name, nanorepo.Asc),
	)
	// E31: the root after another table, a key equated with itself, and a
	// key equated with a value that is not the scope of the caller.
	nanorepo.Search[model.Tenants]("RootSecond", table.Tenants, nanorepo.From(itemsThenTenants))
	nanorepo.Search[model.Items]("SelfEquated", table.Items, nanorepo.From(tenantKeyWithItself))
	nanorepo.Search[model.Items]("ConstantScope", table.Items, nanorepo.From(constantScope))
	nanorepo.Search[model.Items]("BuiltScope", table.Items, nanorepo.From(builtScope))
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

var cfg = struct{ C postgres.BoolExpression }{postgres.Bool(true)}

var anyCond any = postgres.Bool(true)

var byName = map[string]any{"c": postgres.Bool(true)}

type myCond postgres.BoolExpression

var named myCond = postgres.Bool(true)

func structVar() postgres.BoolExpression { return cfg.C }

func anyVar() postgres.BoolExpression { return anyCond.(postgres.BoolExpression) }

func mapVar() postgres.BoolExpression { return byName["c"].(postgres.BoolExpression) }

func namedVar() postgres.BoolExpression { return postgres.BoolExpression(named) }

// extraScope is a scope value with a condition in it.
type extraScope struct {
	TenantID string
	Extra    postgres.BoolExpression
}

func itemsWithExtra(s extraScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).AND(s.Extra))
}

func itemsWithCondition(cond postgres.BoolExpression) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).AND(cond))
}

func subqueryLocal() postgres.BoolExpression {
	t := table.Tenants
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(t).
		WHERE(table.Tenants.Name.EQ(postgres.String("acme"))))
}

func someTenants() postgres.ReadableTable { return table.Tenants }

func subqueryHelper() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(someTenants()))
}

func subqueryConversion() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(postgres.ReadableTable(table.Tenants)))
}

func itemsCrossHelper() postgres.ReadableTable { return table.Items.CROSS_JOIN(someTenants()) }

func methodValue() postgres.BoolExpression {
	f := table.Items.Active.IS_TRUE
	return f()
}

func statement() postgres.BoolExpression {
	return postgres.EXISTS(postgres.UNION(
		postgres.SELECT(table.Items.ID).FROM(table.Items.AS("a")),
		postgres.SELECT(table.Items.ID).FROM(table.Items.AS("b")),
	))
}

func itemsWithUpperAlias() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("Owner"), table.Tenants.AS("Owner").ID.EQ(table.Items.TenantID))
}

func outsideSubquery() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(table.Items.AS("s")).
		WHERE(table.Items.AS("s").TenantID.EQ(table.Items.TenantID))).
		AND(table.Items.AS("s").Active.IS_TRUE())
}

func onLaterJoin() postgres.ReadableTable {
	return table.Items.
		INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
			AND(table.Tenants.Name.EQ(table.Tenants.AS("owner").Name))).
		INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}

func itemsAndOther() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Items.AS("other"), table.Items.AS("other").ID.EQ(table.Items.ID))
}

func tenantsAndOwner() postgres.ReadableTable {
	return table.Tenants.INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Tenants.ID))
}

func itemsThenTenants() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func tenantKeyWithItself() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Tenants.ID))
}

// nameScope stands for the scope value that nanostack gen repo generates.
type nameScope struct{ TenantName string }

var fixedScope = nameScope{TenantName: "acme"}

func constantScope(s nameScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.Name.EQ(postgres.String(fixedScope.TenantName)))
}

func builtScope(s nameScope) postgres.ReadableTable { return joinByName(nameScope{TenantName: "acme"}) }

func joinByName(s nameScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.Name.EQ(postgres.String(s.TenantName)))
}

func noExpression() postgres.BoolExpression {
	var _ = postgres.PRECEDING(1)
	return table.Items.Active.IS_TRUE()
}
