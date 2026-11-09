package builder

import (
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// The Row selects its columns (design S9.1). Each search of this file has
// the shape of a product search: a column subset (echopoint B6), a sort on
// a joined column (echopoint B11), and a LEFT-joined table read into
// pointer fields (anchor #1).

// itemSummary is a subset of the columns of items: the search selects
// these four and no other. The default tiebreak reads ID.
type itemSummary struct {
	ID        string    `alias:"items.id"`
	Name      string    `alias:"items.name"`
	CreatedAt time.Time `alias:"items.created_at"`
	Weight    *float32  `alias:"items.weight"`
}

// itemWithTenant is an item with the name of its tenant: the model of
// items, and one joined column.
type itemWithTenant struct {
	model.Items
	TenantName string `alias:"tenants.name"`
}

// itemWithOwner is an item with the columns of its owner, which a LEFT
// JOIN reads: every owner column is NULL when no owner matches, so each
// field is a pointer.
type itemWithOwner struct {
	model.Items
	OwnerID   *uuid.UUID `alias:"owner.id"`
	OwnerName *string    `alias:"owner.name"`
}

// itemAndTenant embeds two models. Both have ID and Name, so a key reads
// the field through its model: r.Items.Name, r.Tenants.Name. Popularity
// has no alias tag: the hand-written method selects it with the key of the
// SortHandWritten sort.
type itemAndTenant struct {
	model.Items
	model.Tenants
	Popularity int64
}

// itemOfOwner embeds the model of a table that the join reads under an
// alias: the tag names the instance.
type itemOfOwner struct {
	model.Items
	model.Tenants `alias:"owner"`
}

func _() {
	nanorepo.Search[itemSummary]("ItemSummary", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Name, nanorepo.Contains),
		nanorepo.Sort(table.Items.CreatedAt),
		nanorepo.SortKey(table.Items.Weight).NullsLast(),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
	)

	nanorepo.Search[itemWithTenant]("ItemWithTenant", table.Items,
		nanorepo.From(itemsWithTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name"),
		nanorepo.DefaultSort(table.Tenants.Name, nanorepo.Asc),
	)

	nanorepo.Search[itemWithOwner]("ItemWithOwner", table.Items,
		nanorepo.From(itemsWithOwners),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Tenants.AS("owner").Name, nanorepo.Eq),
		nanorepo.SortKey(table.Tenants.AS("owner").Name).API("owner_name").NullsLast(),
		nanorepo.Sort(table.Items.CreatedAt),
	)

	nanorepo.Search[itemAndTenant]("ItemAndTenant", table.Items,
		nanorepo.From(itemsWithTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortKey(table.Items.Name).API("name"),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name"),
		nanorepo.SortHandWritten("popularity"),
	)
}

func _() {
	nanorepo.Search[itemOfOwner]("ItemOfOwner", table.Items,
		nanorepo.From(itemsOfOwners),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortKey(table.Tenants.AS("owner").Name).API("owner_name"),
	)
}

func itemsOfOwners() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}

func itemsWithTenants() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

// itemsWithOwners joins the owner of an active item only: an inactive
// item has no owner row.
func itemsWithOwners() postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID).
		AND(table.Items.Active.IS_TRUE()))
}
