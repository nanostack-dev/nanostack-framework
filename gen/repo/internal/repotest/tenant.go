package repotest

import (
	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// This file declares the searches of the tenant rules that need more than
// a Scope (design S6.4, S9): rows shared by every tenant, a scope inside an
// ON clause, a child filter through EXISTS, and a computed sort key. Their
// methods on ItemRepository are generated, except SearchAssignableLabels,
// and the integration tests run them with two tenants.

// Label is a label as the domain reads it.
type Label struct {
	ID     string
	Shared bool
	Name   string
}

// labelMapper maps a label row. itemRepositoryImpl holds one, so a search
// that maps with (labelMapper).toLabel calls it on that field.
type labelMapper struct{}

func (labelMapper) toLabel(row model.Labels) Label {
	return Label{ID: row.ID, Shared: row.TenantID == nil, Name: row.Name}
}

// itemWithLicense is an item with the plan of its license, which the
// LEFT JOIN reads for the tenant of the scope only.
type itemWithLicense struct {
	model.Items
	Plan *string `alias:"item_licenses.plan"`
}

// itemWithEvents is an item with the number of its events, which a
// correlated COUNT computes.
type itemWithEvents struct {
	model.Items
	EventCount int64
}

func _() {
	// Shaped like anchor #8: the shared labels only (tenant_id IS NULL),
	// filtered on the kinds of their uses through EXISTS.
	nanorepo.Search[model.Labels]("ManagedLabel", table.Labels,
		nanorepo.Map((labelMapper).toLabel),
		nanorepo.ScopeNull(table.Labels.TenantID),
		nanorepo.FilterExists(table.LabelUses, table.LabelUses.LabelID, table.LabelUses.Kind, nanorepo.In),
		nanorepo.Sort(table.Labels.Name),
	)

	// Shaped like anchor #9: the labels of one tenant and the shared ones.
	nanorepo.Search[model.Labels]("AssignableLabel", table.Labels,
		nanorepo.Map((labelMapper).toLabel),
		nanorepo.ScopeOrNull(table.Labels.TenantID),
		nanorepo.FilterExists(table.LabelUses, table.LabelUses.LabelID, table.LabelUses.Kind, nanorepo.In),
		nanorepo.Sort(table.Labels.Name),
	)

	// Shaped like anchor #22: the tenant is in the ON clause of the LEFT
	// JOIN, so an item with no license of that tenant stays.
	nanorepo.Search[itemWithLicense]("LicensedItem", table.Items,
		nanorepo.From(itemsWithLicense),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	// Shaped like echopoint's request_count: a correlated COUNT of the
	// child rows, selected for every order and sortable.
	nanorepo.Search[itemWithEvents]("CountedItem", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortExpr("EventCount", eventCount),
		nanorepo.DefaultSortExpr("EventCount", nanorepo.Desc),
	)
}

func itemsWithLicense(s uuid.UUID) postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.ItemLicenses, table.ItemLicenses.ItemID.EQ(table.Items.ID).
		AND(table.ItemLicenses.TenantID.EQ(postgres.UUID(s))))
}

func eventCount() postgres.IntegerExpression {
	return postgres.IntExp(postgres.SELECT(postgres.COUNT(table.ItemEvents.ID)).
		FROM(table.ItemEvents).
		WHERE(table.ItemEvents.ItemID.EQ(table.Items.ID)))
}
