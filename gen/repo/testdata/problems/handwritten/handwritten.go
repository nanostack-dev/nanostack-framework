// Package handwritten resolves interface methods to hand-written and
// promoted methods: a hand-written method wins over a derivable name, and
// its signature must match the interface.
package handwritten

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Items mixes hand-written, promoted and derived methods.
type Items interface {
	// Derived.
	FindByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[model.Items], error)
	// Hand-written, and the name also derives: the hand-written method wins.
	ExistsByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (bool, error)
	// Hand-written, and the name would not derive.
	FindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (functional.Option[model.Items], error)
	// Hand-written with another signature.
	CountByTenantIDAndStock(ctx context.Context, tenantID uuid.UUID, stock int32) (int64, error)
	// Promoted from an embedded helper.
	Touch(ctx context.Context, id string) error
	// Promoted with another signature.
	ExistsByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (bool, error)
	// Promoted from two fields at one depth.
	Ping(ctx context.Context) error
	// Promoted through two embedded fields of one field.
	Reset(ctx context.Context) error
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, nanorepo.NoMapper[model.Items])
}

type itemsImpl struct {
	itemsBase
	toucher
	skuHelper
	pingA
	pingB
	resetter
}

func (r *itemsImpl) ExistsByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (bool, error) {
	return r.exists(ctx, r.where.TenantID(tenantID).AND(r.where.NameEqualsIgnoreCase(name)))
}

func (r *itemsImpl) FindByTenantIDAndName(
	ctx context.Context, tenantID uuid.UUID, name string,
) (functional.Option[model.Items], error) {
	return r.first(ctx, r.where.TenantID(tenantID).AND(r.where.Name(name)), r.t.ID.ASC())
}

func (r *itemsImpl) CountByTenantIDAndStock(ctx context.Context, tenantID uuid.UUID, stock int64) (int64, error) {
	return r.count(ctx, r.where.TenantID(tenantID).AND(r.where.Stock(int32(stock))))
}

type toucher struct{}

func (toucher) Touch(context.Context, string) error { return nil }

type skuHelper struct{}

func (skuHelper) ExistsByTenantIDAndSku(context.Context, uuid.UUID, int) (bool, error) {
	return false, nil
}

type pingA struct{}

func (pingA) Ping(context.Context) error { return nil }

type pingB struct{}

func (pingB) Ping(context.Context) error { return nil }

type resetter struct {
	resetA
	resetB
}

type resetA struct{}

func (resetA) Reset(context.Context) error { return nil }

type resetB struct{}

func (resetB) Reset(context.Context) error { return nil }
