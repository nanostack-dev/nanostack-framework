// Package buildergap declares a search whose builder does not compile and
// that no check of nano gen repo refuses yet: two scope fields with one
// name (E12 in G6 will refuse it). The compile error is reported at the
// declaration of the search that owns the line, the second of the file.
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
