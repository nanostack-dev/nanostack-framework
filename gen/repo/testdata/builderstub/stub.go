// Package builderstub declares a search that nanostack gen repo cannot write (an
// Eq on a float column has no literal) and whose From function takes the
// scope struct. The
// forward stub has the scope struct and no builder, so the package
// type-checks before a run.
package builderstub

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[model.Items]("Unbuilt", table.Items,
		nanorepo.From(itemsOf),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Scope(table.Items.Sku),
		nanorepo.Filter(table.Items.Price, nanorepo.Eq),
		nanorepo.Sort(table.Items.Name),
	)
}

func itemsOf(s UnbuiltScope) postgres.ReadableTable {
	return table.Items.INNER_JOIN(table.Tenants, table.Tenants.ID.EQ(table.Items.TenantID).
		AND(table.Tenants.ID.EQ(postgres.UUID(s.TenantID))))
}
