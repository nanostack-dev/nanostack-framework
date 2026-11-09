// Package allmapped binds only tables that have a mapper, on columns of
// types outside the model package. So its base must not import the model
// package: Go refuses an unused import.
package allmapped

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/mappers"
)

// Tenants reads tenants.
type Tenants interface {
	Count(ctx context.Context) (int64, error)
}

func _() {
	nanorepo.Implement[Tenants, tenantsImpl](table.Tenants, mappers.TenantToDomain)
}

type tenantsImpl struct{ tenantsBase }

func (r tenantsImpl) Count(ctx context.Context) (int64, error) { return r.count(ctx, nil) }
