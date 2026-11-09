// Package scalaronly binds a table whose derived methods return no row: a
// count, an exists and a delete. So its derived file must not import the
// package of the row type: Go refuses an unused import.
package scalaronly

import (
	"context"

	"github.com/google/uuid"
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Tenants counts, finds and deletes tenants, and returns no row.
type Tenants interface {
	Count(ctx context.Context) (int64, error)
	ExistsByID(ctx context.Context, id uuid.UUID) (bool, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

func _() {
	nanorepo.Implement[Tenants, tenantsImpl](table.Tenants, nanorepo.NoMapper[model.Tenants])
}

type tenantsImpl struct{ tenantsBase }
