// Package apinospec declares a search with an API body, and the run has
// no spec (A7).
package apinospec

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/apibind/apigen"
)

func _() {
	nanorepo.Base[model.Items](table.Items)

	nanorepo.Search[model.Items]("Schedule", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.API[apigen.NoTextRequest](),
	)
}
