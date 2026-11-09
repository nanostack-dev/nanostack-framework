package repotest

import (
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// This file declares searches whose Row is not the model of items (design
// S9.1). nano gen repo writes their builders into row_repo.gen.go, and the
// integration tests walk their joined sorts on PostgreSQL.

// itemSummary is a subset of the columns of items with the name of the
// tenant, which an INNER JOIN reads.
type itemSummary struct {
	ID         string    `alias:"items.id"`
	Name       string    `alias:"items.name"`
	CreatedAt  time.Time `alias:"items.created_at"`
	TenantName string    `alias:"tenants.name"`
}

// itemWithOwner is an item with the name of its owner. A LEFT JOIN reads
// the owner of an active item only, so the name is NULL for an inactive
// item.
type itemWithOwner struct {
	model.Items
	OwnerName *string `alias:"tenants.name"`
}

// itemOfOwner embeds the model of the tenant, which the join reads under
// the alias owner: the tag names the instance, so the scan fills it.
type itemOfOwner struct {
	model.Items
	model.Tenants `alias:"owner"`
}

func _() {
	nanorepo.Search[itemSummary]("ItemSummary", table.Items,
		nanorepo.From(itemsWithTenants),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Tenants.Name).API("tenant_name"),
	)

	nanorepo.Search[itemWithOwner]("ItemWithOwner", table.Items,
		nanorepo.From(itemsWithOwners),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Tenants.Name, nanorepo.Present),
		nanorepo.SortKey(table.Tenants.Name).API("owner_name").NullsLast(),
	)

	nanorepo.Search[itemOfOwner]("ItemOfOwner", table.Items,
		nanorepo.From(itemsOfOwners),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.SortKey(table.Tenants.AS("owner").Name).API("owner_name"),
	)
}

func itemsWithTenants() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID))
}

func itemsOfOwners() postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants.AS("owner"), table.Tenants.AS("owner").ID.EQ(table.Items.TenantID))
}

// itemsWithOwners joins the tenant of an active item only.
func itemsWithOwners() postgres.ReadableTable {
	return table.Items.LEFT_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Items.Active.IS_TRUE()))
}
