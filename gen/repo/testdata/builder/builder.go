// Package builder declares searches that nanostack gen repo writes in full: the
// builder goldens, the build and vet check, and the line count check read
// it. use.go names the generated builders, as a handler and a service do.
package builder

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
)

// sku is a named type over the model field of items.sku.
type sku string

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	// One scope value: the scope is a uuid.UUID, not a struct. Every
	// operator that has a field type, and a key of every kind of column
	// the table has.
	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Condition(listed),
		nanorepo.Filter(table.Items.ID, nanorepo.In),
		nanorepo.Filter(table.Items.Status, nanorepo.Eq, nanorepo.In),
		nanorepo.FilterAs[sku](table.Items.Sku, nanorepo.In.API("skus")),
		nanorepo.Filter(table.Items.Name, nanorepo.Contains, nanorepo.ContainsAny),
		nanorepo.Filter(table.Items.Active, nanorepo.Eq),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("created_since", "created_until")),
		nanorepo.GoOnly(nanorepo.Filter(table.Items.DeletedAt, nanorepo.Present)),
		nanorepo.FilterExpr("LowerName", lowerName, nanorepo.InFold),
		nanorepo.Flag("Expensive", expensive),
		nanorepo.FullText(table.Items.Name, table.Items.Sku),
		nanorepo.FullTextExpr(skuText),
		nanorepo.Sort(table.Items.Name, table.Items.CreatedAt, table.Items.Stock, table.Items.Sold),
		nanorepo.Sort(table.Items.Price, table.Items.Status, table.Items.LocalAt),
		nanorepo.SortKey(table.Items.Weight).API("weight", "mass").NullsLast(),
		nanorepo.SortKey(table.Items.DeletedAt).NullsFirst(),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
		nanorepo.DefaultLimit(50),
	)

	// The tenant comes from the context only: the scope is struct{}, the
	// constructor takes no argument, and a From function takes struct{}.
	nanorepo.Search[model.Boards]("Board", table.Boards,
		nanorepo.From(allBoards),
		nanorepo.ScopeFunc(workspacescope.Filter, table.Boards.WorkspaceID),
		nanorepo.Filter(table.Boards.ID, nanorepo.Eq, nanorepo.In),
		nanorepo.Sort(table.Boards.Title),
	)
}

func allBoards(struct{}) postgres.ReadableTable { return table.Boards }

func listed() postgres.BoolExpression { return table.Items.DeletedAt.IS_NULL() }

func lowerName() postgres.StringExpression { return postgres.LOWER(table.Items.Name) }

func expensive() postgres.BoolExpression { return table.Items.Price.GT(postgres.Float(100)) }

func skuText() postgres.StringExpression {
	return postgres.StringExp(postgres.CAST(table.Items.Sku).AS("text"))
}
