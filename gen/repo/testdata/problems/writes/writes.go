// Package writes declares write methods whose names do not derive, one per
// rule of the write grammar: each is an error at its line, with a fix. One
// upsert derives, so the run also shows what a derived write looks like.
package writes

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Item is the domain type of an item.
type Item struct {
	ID       string
	TenantID uuid.UUID
	Sku      string
}

func itemToDomain(row model.Items) Item {
	return Item{ID: row.ID, TenantID: row.TenantID, Sku: row.Sku}
}

func itemToRow(i Item) model.Items { return model.Items{ID: i.ID, TenantID: i.TenantID, Sku: i.Sku} }

// Items breaks one write rule per method; UpsertByTenantIDAndSku derives.
type Items interface {
	// A Create on a scoped table names its scope.
	Create(ctx context.Context, item Item) (Item, error)
	// CreateIn names scope columns only.
	CreateInTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, item Item) (Item, error)
	// The value is the domain type, not a pointer to it.
	CreateInTenantID(ctx context.Context, tenantID uuid.UUID, item *Item) error
	// An update filters on the scope first.
	UpdateBySku(ctx context.Context, item Item) error
	// An update changes one row: (tenant_id, name) is no key.
	UpdateByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, item Item) (Item, error)
	// A write never ends in Internal.
	UpdateByTenantIDAndIDInternal(ctx context.Context, tenantID uuid.UUID, item Item) error
	// An update returns the row, an Option of it, or an error.
	UpdateByTenantIDAndID(ctx context.Context, tenantID uuid.UUID, item Item) (bool, error)
	// An upsert conflicts on a key that holds the tenant: sku alone would let
	// one tenant overwrite another's row.
	UpsertBySku(ctx context.Context, item Item) (Item, error)
	// An upsert conflicts on exactly one unique key: (tenant_id) is none.
	UpsertByTenantID(ctx context.Context, tenantID uuid.UUID, item Item) error
	// Derived: ON CONFLICT (tenant_id, sku).
	UpsertByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, item Item) (Item, error)
}

// ItemsWithoutToRow has no nanorepo.ToRow, so its writes do not derive.
type ItemsWithoutToRow interface {
	CreateInTenantID(ctx context.Context, tenantID uuid.UUID, item Item) (Item, error)
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, itemToDomain, nanorepo.ToRow(itemToRow))
	nanorepo.Implement[ItemsWithoutToRow, itemsWithoutToRowImpl](table.Items, itemToDomain)
}

type itemsImpl struct{ itemsBase }

// NewItems returns the repository on db.
func NewItems(db qrm.DB) Items { return &itemsImpl{itemsBase: newItemsBase(db)} }

type itemsWithoutToRowImpl struct{ itemsBase }

// NewItemsWithoutToRow returns the repository on db.
func NewItemsWithoutToRow(db qrm.DB) ItemsWithoutToRow {
	return &itemsWithoutToRowImpl{itemsBase: newItemsBase(db)}
}
