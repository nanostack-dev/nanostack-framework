// Package textops declares InFold and ContainsAny on columns that are not
// text (E2): LOWER and ILIKE fail on uuid and inet at run time.
package textops

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.TenantID, nanorepo.InFold),
		nanorepo.Filter(table.Items.Status, nanorepo.ContainsAny),
		nanorepo.Sort(table.Items.Name),
	)
}
