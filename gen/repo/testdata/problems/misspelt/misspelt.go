// Package misspelt names a column that its table does not have, in a search
// (E1). go vet fails first; nano gen repo names the column it meant.
package misspelt

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Sort(table.Items.Nmae),
		nanorepo.Filter(table.Items.CreatdAt, nanorepo.Range()),
	)
}
