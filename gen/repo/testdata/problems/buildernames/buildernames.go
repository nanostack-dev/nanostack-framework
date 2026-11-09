// Package buildernames declares hand-written names that a builder would
// declare, and two searches whose builders share a name.
package buildernames

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// ItemSort is hand-written, and the builder of search Item declares it.
type ItemSort int

// Status is a hand-written method of the builder with the name of the
// accessor of the filter Status.
func (q ItemSearch) Status() string { return "" }

// FullText is a hand-written method of the builder, which declares
// FullText for its full-text expressions.
func (q *ItemSearch) FullText(string) {}

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// The sort "search" makes the constant ItemSortSearch: the builder type
	// of search ItemSort.
	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Status, nanorepo.Eq),
		nanorepo.FullText(table.Items.Name),
		nanorepo.Sort(table.Items.Name),
		nanorepo.SortKey(table.Items.Sku).API("search"),
	)

	nanorepo.Search[model.Items]("ItemSort", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)
}
