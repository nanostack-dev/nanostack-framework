// Package writedecl passes the options of nanorepo.Implement wrong, one
// mistake per declaration: each is an error at its line, with a fix.
package writedecl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Item is the domain type of an item.
type Item struct{ ID string }

// Other is another domain type.
type Other struct{ ID string }

func itemToDomain(row model.Items) Item { return Item{ID: row.ID} }

func itemToRow(i Item) model.Items { return model.Items{ID: i.ID} }

func otherToRow(o Other) model.Items { return model.Items{ID: o.ID} }

// errExists is a package-level error.
var errExists = errors.New("exists")

// notAnError is a package-level variable that is not an error.
var notAnError = "exists"

// option is an option kept in a variable.
var option = nanorepo.ToRow(itemToRow)

// Repo is bound six times, each with one mistake.
type Repo interface {
	CreateInTenantID(ctx context.Context, tenantID uuid.UUID, item Item) (Item, error)
}

type (
	unknownConstraint struct{}
	notErrorTarget    struct{}
	wrongToRow        struct{}
	twoToRows         struct{}
	toRowWithNoMapper struct{}
	optionInVariable  struct{}
)

func _() {
	// No such unique key or index on items.
	nanorepo.Implement[Repo, unknownConstraint](table.Items, itemToDomain, nanorepo.ToRow(itemToRow),
		nanorepo.OnUnique(errExists, "items_sku_key"))
	// The target is not an error.
	nanorepo.Implement[Repo, notErrorTarget](table.Items, itemToDomain,
		nanorepo.OnUnique(errors.New(notAnError), "items_pkey"))
	// ToRow maps another domain type than toDomain returns.
	nanorepo.Implement[Repo, wrongToRow](table.Items, itemToDomain, nanorepo.ToRow(otherToRow))
	// ToRow twice.
	nanorepo.Implement[Repo, twoToRows](table.Items, itemToDomain, nanorepo.ToRow(itemToRow),
		nanorepo.ToRow(itemToRow))
	// ToRow with NoMapper, whose writes take the model.
	nanorepo.Implement[Repo, toRowWithNoMapper](table.Tenants, nanorepo.NoMapper[model.Tenants],
		nanorepo.ToRow(func(t model.Tenants) model.Tenants { return t }))
	// An option kept in a variable cannot be read from source.
	nanorepo.Implement[Repo, optionInVariable](table.Items, itemToDomain, option)
}
