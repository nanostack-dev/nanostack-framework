// Package derive declares interface methods whose names do not derive, one
// per rule of the grammar: each is an error at its line, with a fix.
package derive

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Item is the domain type of an item.
type Item struct{ ID string }

// Tenant is another domain type.
type Tenant struct{ ID string }

func itemToDomain(row model.Items) Item { return Item{ID: row.ID} }

// Items breaks one rule per method.
type Items interface {
	Pinger
	// Outside the grammar, with a derivable rename.
	FindActive(ctx context.Context, tenantID uuid.UUID, status model.ItemStatus) ([]Item, error)
	// Outside the grammar, and no derived method has its results.
	Reserve(ctx context.Context, tenantID uuid.UUID, sku string) (Item, bool, error)
	// An operator, an OrderBy and an Or, with the hand-written body.
	FindByTenantIDAndNameIgnoreCase(ctx context.Context, tenantID uuid.UUID, name string) (functional.Option[Item], error)
	FindByTenantIDAndSkuIn(ctx context.Context, tenantID uuid.UUID, skus []string) ([]Item, error)
	FindByTenantIDOrderByCreatedAtDesc(ctx context.Context, tenantID uuid.UUID) ([]Item, error)
	FindByTenantIDAndSkuOrName(ctx context.Context, tenantID uuid.UUID, sku, name string) ([]Item, error)
	// No such column: a misspelling, an abbreviation, nothing close.
	FindByTenantIdAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error)
	CountByTenantIDAndExtID(ctx context.Context, tenantID uuid.UUID, externalID string) (int64, error)
	CountByTenantIDAndColour(ctx context.Context, tenantID uuid.UUID, colour string) (int64, error)
	FindBy(ctx context.Context) ([]Item, error)
	// One row, no unique key.
	FindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (functional.Option[Item], error)
	GetByTenantIDAndStatus(ctx context.Context, tenantID uuid.UUID, status model.ItemStatus) (Item, error)
	// Parameters.
	FindByTenantIDAndSkuAndName(ctx context.Context, tenantID uuid.UUID, sku string) ([]Item, error)
	ExistsByTenantIDAndExternalID(ctx context.Context, tenantID uuid.UUID, externalID *string) (bool, error)
	ExistsByTenantIDAndSkuAndName(ctx context.Context, tenantID uuid.UUID, name, sku string) (bool, error)
	CountByTenantIDAndStock(ctx context.Context, tenantID uuid.UUID, stock int64) (int64, error)
	CountByTenantIDAndTags(ctx context.Context, tenantID uuid.UUID, tags string) (int64, error)
	FindByTenantIDAndRank(tenantID uuid.UUID, rank int16) ([]Item, error)
	// Results.
	CountByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (bool, error)
	ExistsByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (int64, error)
	FindByTenantIDAndPrice(ctx context.Context, tenantID uuid.UUID, price float64) ([]Tenant, error)
	DeleteByTenantIDAndRank(ctx context.Context, tenantID uuid.UUID, rank int16) (string, error)
	FindByTenantIDAndActive(ctx context.Context, tenantID uuid.UUID, active bool) []Item
	// Tenant scope.
	FindByIDAndTenantID(ctx context.Context, id string, tenantID uuid.UUID) (functional.Option[Item], error)
	ExistsBySku(ctx context.Context, sku string) (bool, error)
	ExistsByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (bool, error)
	FindAll(ctx context.Context) ([]Item, error)
	DeleteBySku(ctx context.Context, sku string) error
	DeleteBySkuInternal(ctx context.Context, sku string) (int64, error)
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, itemToDomain)
}

type itemsImpl struct{ itemsBase }

// NewItems returns the items on db.
func NewItems(db qrm.DB) Items { return &itemsImpl{itemsBase: newItemsBase(db)} }
