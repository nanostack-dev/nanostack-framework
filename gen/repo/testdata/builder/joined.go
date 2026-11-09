package builder

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// This file declares only a search: nano gen repo writes joined_repo.gen.go
// for it all the same.
func _() {
	// Two scope fields make the struct ItemOfTenantScope. One Scope with two
	// columns makes one field; ScopeOrNull makes a field too, and ScopeNull
	// none. The From function takes the scope value and puts a field of it
	// in the ON clause.
	nanorepo.Search[model.Items]("ItemOfTenant", table.Items,
		nanorepo.From(itemsOfTenant),
		nanorepo.Scope(table.Items.TenantID, table.Tenants.ID),
		nanorepo.ScopeOrNull(table.Items.ExternalID),
		nanorepo.ScopeNull(table.Items.ReleasedOn),
		nanorepo.Unscoped(table.Tenants.Name, "the ON clause scopes it"),
		nanorepo.Filter(table.Tenants.Name, nanorepo.Eq),
		nanorepo.Filter(table.Items.Stock, nanorepo.Eq, nanorepo.In),
		nanorepo.SortKey(table.Items.Name).API("name", "title"),
		nanorepo.SortHandWritten("popularity"),
		nanorepo.Tiebreak(table.Items.Sku, table.Items.ID),
		nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc),
	)
}

func itemsOfTenant(s ItemOfTenantScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Tenants.ID.EQ(postgres.UUID(s.TenantID))))
}
