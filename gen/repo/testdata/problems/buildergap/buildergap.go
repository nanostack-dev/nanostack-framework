// Package buildergap declares the search whose builder did not compile
// before the tenant check: two scope fields with one name. E12 refuses it
// now, before the builder is written.
package buildergap

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[model.Items]("Fine", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)

	nanorepo.Search[model.Items]("Twice", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.ScopeOrNull(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)
}
