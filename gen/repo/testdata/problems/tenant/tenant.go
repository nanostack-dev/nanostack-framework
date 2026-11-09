// Package tenant declares the searches that the tenant check refuses
// (design S6), one per attack of the tenant review, with the FilterExists
// link (G7) next to them. Each search has no
// other problem, so the tenant check runs on each.
package tenant

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
)

// everyRow returns TRUE: a scope function that scopes nothing.
func everyRow(context.Context, postgres.ColumnString) postgres.BoolExpression {
	return postgres.Bool(true)
}

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// INNER_JOIN(items, Bool(true)): the join links no row to a scoped one.
	nanorepo.Search[model.AuditEvents]("JoinOnTrue", table.AuditEvents,
		nanorepo.From(eventsJoinedOnTrue),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.AuditEvents.At),
	)

	// A wrong ON column: items.id = audit_events.note matches no declared
	// foreign key, so audit_events is not linked to the scoped items.
	nanorepo.Search[model.AuditEvents]("WrongOnColumn", table.AuditEvents,
		nanorepo.From(eventsOnTheNote),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.AuditEvents.At),
	)

	// AS("other"): a second instance of items, which the scope of items
	// does not cover.
	nanorepo.Search[model.Items]("SecondAlias", table.Items,
		nanorepo.From(itemsAndAnother),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// A ScopeFunc that returns TRUE on a hard scope.
	nanorepo.Search[model.Items]("FuncOnHard", table.Items,
		nanorepo.ScopeFunc(everyRow, table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// A context scope with a function that project.go does not register.
	nanorepo.Search[model.Boards]("UnregisteredFunc", table.Boards,
		nanorepo.ScopeFunc(everyRow, table.Boards.WorkspaceID),
		nanorepo.Sort(table.Boards.Title),
	)

	// A context scope through Scope instead of its registered ScopeFunc.
	nanorepo.Search[model.Boards]("ScopeOnContext", table.Boards,
		nanorepo.Scope(table.Boards.WorkspaceID),
		nanorepo.Sort(table.Boards.Title),
	)

	// A correlation on a column pair that is no foreign key, as
	// method = webhooks.name.
	nanorepo.Search[model.Items]("WrongCorrelation", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Flag("HasEvents", eventsByName),
		nanorepo.Sort(table.Items.Name),
	)

	// The subquery reads items with no alias: SQL binds both sides of the
	// correlation to the inner row.
	nanorepo.Search[model.Items]("SameNameInner", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Flag("HasTwin", twinItem),
		nanorepo.Sort(table.Items.Name),
	)

	// An OR in the ON clause: the tenant equality under it counts for
	// nothing.
	nanorepo.Search[model.Items]("OrInOn", table.Items,
		nanorepo.From(itemsAndLicenseWithOr),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// The child and the parent tenant_id in two Scope calls: one field
	// name for two values.
	nanorepo.Search[model.Items]("TwoFieldsOneName", table.Items,
		nanorepo.From(itemsAndTheirLicense),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.ItemLicenses.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// A WHERE scope on the LEFT-joined table drops the items with no
	// license.
	nanorepo.Search[model.Items]("ScopeOnLeft", table.Items,
		nanorepo.From(itemsAndOptionalLicense),
		nanorepo.Scope(table.Items.TenantID, table.ItemLicenses.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// Unscoped as the only coverage of items.
	nanorepo.Search[model.Items]("UnscopedAlone", table.Items,
		nanorepo.Unscoped(table.Items.TenantID, "read every tenant"),
		nanorepo.Sort(table.Items.Name),
	)

	// The registered context scope is accepted; the hard scope is missing.
	nanorepo.Search[model.Labels]("NoScope", table.Labels,
		nanorepo.Sort(table.Labels.Name),
	)

	// FilterExists on a link that is no foreign key to the searched table.
	nanorepo.Search[model.Labels]("ExistsNotLinked", table.Labels,
		nanorepo.ScopeOrNull(table.Labels.TenantID),
		nanorepo.FilterExists(table.LabelUses, table.LabelUses.Kind, table.LabelUses.Kind, nanorepo.In),
		nanorepo.Sort(table.Labels.Name),
	)

	// projects has a hard tenant column and a nullable context one: the
	// hard scope alone leaves workspace_id uncovered.
	nanorepo.Search[model.Projects]("ProjectTenantOnly", table.Projects,
		nanorepo.Scope(table.Projects.TenantID),
		nanorepo.Sort(table.Projects.Name),
	)

	// The context scope does not cover a table whose hard tenant column is
	// unscoped.
	nanorepo.Search[model.Projects]("ProjectUnscopedTenant", table.Projects,
		nanorepo.Unscoped(table.Projects.TenantID, "read every tenant"),
		nanorepo.ScopeFunc(workspacescope.Filter, table.Projects.WorkspaceID),
		nanorepo.Sort(table.Projects.Name),
	)

	// Accepted: the registered context scope.
	nanorepo.Search[model.Boards]("Board", table.Boards,
		nanorepo.ScopeFunc(workspacescope.Filter, table.Boards.WorkspaceID),
		nanorepo.Sort(table.Boards.Title),
	)

	// Two scope fields: the scope type is a struct with keyed fields only.
	nanorepo.Search[model.Items]("Pair", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.Items.Sku),
		nanorepo.Sort(table.Items.Name),
	)
}

// pair builds the scope of Pair with unkeyed fields: two values of one
// type could change places.
func pair(tenant uuid.UUID, sku string) PairScope { return PairScope{struct{}{}, tenant, sku} }

var _ = pair

func eventsJoinedOnTrue() postgres.ReadableTable {
	return table.AuditEvents.INNER_JOIN(table.Items, table.Items.ID.EQ(table.AuditEvents.ItemID).OR(postgres.Bool(true)))
}

func eventsOnTheNote() postgres.ReadableTable {
	return table.AuditEvents.INNER_JOIN(table.Items, table.Items.ID.EQ(table.AuditEvents.Note))
}

func itemsAndAnother() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Items.AS("other"), table.Items.AS("other").ID.EQ(table.Items.ID))
}

func eventsByName() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.Name)))
}

func twinItem() postgres.BoolExpression {
	return postgres.EXISTS(postgres.SELECT(postgres.Int(1)).FROM(table.Items).
		WHERE(table.Items.TenantID.EQ(table.Items.TenantID)))
}

func itemsAndLicenseWithOr() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.ItemLicenses, table.ItemLicenses.ItemID.EQ(table.Items.ID).
		AND(table.ItemLicenses.TenantID.EQ(table.Items.TenantID).OR(postgres.Bool(true))))
}

func itemsAndTheirLicense() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.ItemLicenses, table.ItemLicenses.ItemID.EQ(table.Items.ID))
}

func itemsAndOptionalLicense() postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.ItemLicenses, table.ItemLicenses.ItemID.EQ(table.Items.ID))
}
