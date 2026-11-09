// Package row declares searches whose Row nanostack gen repo refuses (design
// S9.1): a key column with no Row field and a Row field that selects
// nothing (E25), and a field that cannot hold the NULL its column can be
// (E26). Each search shows one shape.
package row

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// sku is a named type over the model field of items.sku.
type sku string

// withoutTenant has no field for tenants.name, which a sort reads.
type withoutTenant struct {
	model.Items
	Listed bool `alias:"items.active"`
}

// noID has no field for items.id, which the default tiebreak reads.
type noID struct {
	Name string `alias:"items.name"`
}

// plain has a field with no alias tag, and the search has no
// SortHandWritten sort whose key could select it.
type plain struct {
	model.Items
	Score int64
}

// unexported has a field that the scan cannot set.
type unexported struct {
	model.Items
	tenant string `alias:"tenants.name"`
}

// badTags has tags that name no column the search reads: another
// spelling of items.name, and a table the search does not read.
type badTags struct {
	ID    string `alias:"items.id"`
	Title string `alias:"Items.Name"`
	Note  string `alias:"audit_events.note"`
}

// wrongType reads an integer column into a string.
type wrongType struct {
	model.Items
	Count string `alias:"items.stock"`
}

// leftValue reads a NOT NULL column of a LEFT-joined table into a value.
type leftValue struct {
	model.Items
	TenantName string `alias:"tenants.name"`
}

// leftEmbedded embeds the model of a LEFT-joined table, whose fields are
// not pointers.
type leftEmbedded struct {
	model.Items
	model.Tenants
}

// nullValue reads a nullable column into a value.
type nullValue struct {
	ID     string  `alias:"items.id"`
	Weight float32 `alias:"items.weight"`
}

// pointerEmbedded embeds a pointer to a model.
type pointerEmbedded struct {
	model.Items
	*model.Tenants
}

// ownerUntagged embeds the model of a table that the search reads only
// under an alias.
type ownerUntagged struct {
	model.Items
	model.Tenants
}

// foreign embeds the model of a table that the search does not read.
type foreign struct {
	model.Items
	model.AuditEvents
}

// empty selects no column: the key of the SortHandWritten sort fills
// Popularity, and nothing fills the row.
type empty struct {
	Popularity int64
}

// keyTypes reads key columns into fields of other types: a pointer to a
// NOT NULL column, and a named type over the model's string.
type keyTypes struct {
	ID   string  `alias:"items.id"`
	Name *string `alias:"items.name"`
	Sku  sku     `alias:"items.sku"`
}

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[withoutTenant]("MissingJoined", table.Items,
		nanorepo.From(itemsWithTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Tenants.Name),
	)

	nanorepo.Search[noID]("MissingTiebreak", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[plain]("Plain", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[unexported]("Unexported", table.Items,
		nanorepo.From(itemsWithTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[badTags]("BadTags", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.ID),
	)

	nanorepo.Search[wrongType]("WrongType", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[leftValue]("LeftValue", table.Items,
		nanorepo.From(itemsMaybeTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Tenants.Name),
	)

	nanorepo.Search[leftEmbedded]("LeftEmbedded", table.Items,
		nanorepo.From(itemsMaybeTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name"),
	)

	nanorepo.Search[nullValue]("NullValue", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Weight),
	)

	nanorepo.Search[pointerEmbedded]("PointerEmbedded", table.Items,
		nanorepo.From(itemsMaybeTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[ownerUntagged]("OwnerUntagged", table.Items,
		nanorepo.From(itemsWithOwners),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.CreatedAt),
	)

	nanorepo.Search[foreign]("Foreign", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.CreatedAt),
	)

	nanorepo.Search[empty]("Empty", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortHandWritten("popularity"),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[keyTypes]("KeyTypes", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name, table.Items.Sku),
	)
}

func itemsWithTenants() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsMaybeTenants() postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsWithOwners() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}
