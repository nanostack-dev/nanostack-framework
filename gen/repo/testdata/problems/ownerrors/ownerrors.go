// Package ownerrors has compile errors of its own beside a method that does
// not resolve: one run reports both, the compile errors at their lines, and
// a method whose signature does not type-check gets no fix of its own.
package ownerrors

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Items has a method with an undefined type and one that does not derive.
type Items interface {
	FindByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku Skuu) ([]model.Items, error)
	FindActive(ctx context.Context, tenantID uuid.UUID) ([]model.Items, error)
	CountByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (int64, error)
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, nanorepo.NoMapper[model.Items])
}

type itemsImpl struct{ itemsBase }

// NewItems returns the items on db. Its missing method repeats the
// FindActive error, so it is not reported again.
func NewItems(db qrm.DB) Items { return &itemsImpl{itemsBase: newItemsBase(db)} }

func (r *itemsImpl) recent(ctx context.Context) ([]model.Items, error) {
	return r.findAll(ctx, nil, r.t.CreatedAt.DESC(), r.t.Nmae.ASC())
}
