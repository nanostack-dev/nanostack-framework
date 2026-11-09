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
		nanorepo.Filter(table.Tenants.Name, nanorepo.Eq),
		nanorepo.Filter(table.Items.Stock, nanorepo.Eq, nanorepo.In),
		nanorepo.SortKey(table.Items.Name).API("name", "title"),
		nanorepo.SortHandWritten("popularity"),
		nanorepo.Tiebreak(table.Items.Sku, table.Items.ID),
		nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc),
	)
}

func _() {
	// The shape of design section 4.2: a join, a scope struct, six filters, a FullText of four parts, three sorts and a
	// tiebreak of two columns. The line count check reads its builder.
	nanorepo.Search[model.Items]("Shaped", table.Items,
		nanorepo.From(itemsWithTenant),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.Items.ExternalID),
		nanorepo.Filter(table.Items.ID, nanorepo.In),
		nanorepo.FilterAs[sku](table.Items.Sku, nanorepo.In),
		nanorepo.FilterExpr("LowerName", lowerName, nanorepo.In),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("since", "until")),
		nanorepo.Filter(table.Items.DeletedAt, nanorepo.Present),
		nanorepo.Flag("Expensive", expensive),
		nanorepo.FullText(table.Items.Name),
		nanorepo.FullTextExpr(skuText, lowerName, tenantName),
		nanorepo.Sort(table.Items.CreatedAt, table.Items.Status, table.Items.Stock),
		nanorepo.Tiebreak(table.Items.Sku, table.Items.ID),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
	)
}

func itemsWithTenant(s ShapedScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Tenants.ID.EQ(postgres.UUID(s.TenantID))))
}

func tenantName() postgres.StringExpression { return table.Tenants.Name }

func itemsOfTenant(s ItemOfTenantScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Tenants.ID.EQ(postgres.UUID(s.TenantID))))
}
