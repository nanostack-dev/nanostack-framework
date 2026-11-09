// Package shared declares two repositories on one table, which share its
// base, with a method value on a type without fields as the mapper, and a
// repository whose mapper is a function of another package.
package shared

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/mappers"
)

type emptyMapper struct{}

func (emptyMapper) ToDomain(row model.Items) mappers.Item { return mappers.ItemToDomain(row) }

var itemToDomain = emptyMapper{}.ToDomain

// Reader reads items.
type Reader interface {
	Find(ctx context.Context, id string) (mappers.Item, error)
}

// Writer deletes items.
type Writer interface {
	Delete(ctx context.Context, id string) error
}

// Tenants reads tenants.
type Tenants interface {
	Count(ctx context.Context) (int64, error)
}

func _() {
	nanorepo.Implement[Reader, readerImpl](table.Items, itemToDomain)
	nanorepo.Implement[Writer, writerImpl](table.Items, itemToDomain)
	nanorepo.Implement[Tenants, tenantsImpl](table.Tenants, mappers.TenantToDomain)
}

type readerImpl struct{ itemsBase }

func (r readerImpl) Find(ctx context.Context, id string) (mappers.Item, error) {
	return r.queryRow(ctx, r.t.SELECT(r.t.AllColumns).WHERE(r.where.ID(id))).Value()
}

type writerImpl struct{ itemsBase }

func (w writerImpl) Delete(ctx context.Context, id string) error {
	_, err := w.deleteWhere(ctx, w.where.ID(id))
	return err
}

type tenantsImpl struct{ tenantsBase }

func (r tenantsImpl) Count(ctx context.Context) (int64, error) { return r.count(ctx, nil) }
