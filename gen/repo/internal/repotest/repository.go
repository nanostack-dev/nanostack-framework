// Package repotest is the fixture of nano gen repo: repositories declared on
// the shop schema (migrations/), whose go-jet code nano gen db wrote into
// db/shop, and the repo_base.gen.go nano gen repo writes for them. The
// generator's tests check that the committed base is current, and the
// integration tests run every helper of the base on PostgreSQL.
package repotest

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/enum"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Scopes are the tenant columns of the fixture, as Postgres.Scopes declares
// them in a service.
var Scopes = []string{"tenant_id"} //nolint:gochecknoglobals // fixture

// ErrItemSKUExists is returned when a tenant already has an item with the SKU.
var ErrItemSKUExists = errors.New("an item with this SKU exists")

// itemSKUKey is the unique constraint on (tenant_id, sku).
const itemSKUKey = "items_tenant_sku_key"

// Item is the domain type of an item.
type Item struct {
	ID       string
	TenantID uuid.UUID
	SKU      string
	Name     string
	Status   model.ItemStatus
	Stock    int32
}

// itemToDomain maps a row of items.
func itemToDomain(row model.Items) Item {
	return Item{ID: row.ID, TenantID: row.TenantID, SKU: row.Sku, Name: row.Name, Status: row.Status, Stock: row.Stock}
}

// ItemRepository reads and writes the items of a tenant.
type ItemRepository interface {
	FindBySKU(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error)
	ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error)
	Create(ctx context.Context, item model.Items) (Item, error)
	Archive(ctx context.Context, tenantID uuid.UUID, id string) (bool, error)
	CountInternal(ctx context.Context) (int64, error)
}

// AuditLog appends audit events. audit_events has no tenant column.
type AuditLog interface {
	Append(ctx context.Context, itemID, action string) (model.AuditEvents, error)
}

func _() {
	nanorepo.Implement[ItemRepository, itemRepositoryImpl](table.Items, itemToDomain)
	nanorepo.Implement[AuditLog, auditLogImpl](table.AuditEvents, nanorepo.NoMapper[model.AuditEvents])
	nanorepo.Base[model.Tenants](table.Tenants)
}

type itemRepositoryImpl struct {
	itemsBase
}

// NewItemRepository returns the item repository on db.
func NewItemRepository(db qrm.DB) ItemRepository {
	return &itemRepositoryImpl{itemsBase: newItemsBase(db)}
}

func (r *itemRepositoryImpl) FindBySKU(
	ctx context.Context, tenantID uuid.UUID, sku string,
) (functional.Option[Item], error) {
	return r.findOne(ctx, r.where.TenantID(tenantID).AND(r.where.Sku(sku)))
}

func (r *itemRepositoryImpl) ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error) {
	return r.findAll(ctx, r.where.TenantID(tenantID).AND(r.where.Status(model.ItemStatus_Active)),
		r.t.Sku.ASC(), r.t.ID.ASC())
}

func (r *itemRepositoryImpl) Create(ctx context.Context, item model.Items) (Item, error) {
	stmt := r.t.INSERT(r.t.AllColumns).MODEL(item).RETURNING(r.t.AllColumns)
	return r.queryRow(ctx, stmt).OnUnique(ErrItemSKUExists, itemSKUKey).Value()
}

func (r *itemRepositoryImpl) Archive(ctx context.Context, tenantID uuid.UUID, id string) (bool, error) {
	n, err := r.updateWhere(ctx, r.where.TenantID(tenantID).AND(r.where.ID(id)),
		r.t.Status.SET(enum.ItemStatus.Archived))
	return n > 0, err
}

// CountInternal counts the items of every tenant: a trusted system path.
func (r *itemRepositoryImpl) CountInternal(ctx context.Context) (int64, error) {
	return r.count(ctx, nil)
}

type auditLogImpl struct {
	auditEventsBase
}

// NewAuditLog returns the audit log on db.
func NewAuditLog(db qrm.DB) AuditLog {
	return &auditLogImpl{auditEventsBase: newAuditEventsBase(db)}
}

func (a *auditLogImpl) Append(ctx context.Context, itemID, action string) (model.AuditEvents, error) {
	stmt := a.t.INSERT(a.t.ItemID, a.t.Action).VALUES(itemID, action).RETURNING(a.t.AllColumns)
	return a.queryRow(ctx, stmt).Value()
}
