// Package repotest is the fixture of nanostack gen repo: repositories declared on
// the shop schema (migrations/), whose go-jet code nanostack gen db wrote into
// db/shop, and the files nanostack gen repo writes for them: repo_base.gen.go and
// repository_repo.gen.go, with the derived methods. The generator's tests
// check that the committed files are current, and the integration tests run
// every helper of the base and every derived method on PostgreSQL.
package repotest

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/enum"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Scopes are the tenant columns of the fixture, as Postgres.Scopes declares
// them in a service.
var Scopes = []string{"tenant_id", "workspace_id"} //nolint:gochecknoglobals // fixture

// ContextScopes registers the function that scopes each context tenant
// column of the fixture, as Postgres.ContextScopes declares it.
var ContextScopes = map[string]string{ //nolint:gochecknoglobals // fixture
	"workspace_id": "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Filter",
}

// ContextStamps registers the function that stamps a new row with each
// context tenant column of the fixture, as ContextScope.Stamp declares it.
var ContextStamps = map[string]string{ //nolint:gochecknoglobals // fixture
	"workspace_id": "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Stamp",
}

// DatabaseColumns are the columns the database writes, as
// Postgres.DatabaseColumns declares them.
var DatabaseColumns = []string{"created_at", "updated_at"} //nolint:gochecknoglobals // fixture

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

// TenantID is a named type over the model type of tenant_id: a derived
// method converts it.
type TenantID uuid.UUID

// ItemRepository reads and writes the items of a tenant. The methods whose
// names derive are generated in repository_repo.gen.go; the others are
// hand-written below.
type ItemRepository interface {
	// Derived. A single-row find is proven by a key: items_tenant_sku_key
	// (tenant_id, sku), or items_pkey (id).
	FindByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error)
	FindByTenantIDAndID(ctx context.Context, tenantID uuid.UUID, id string) (functional.Option[Item], error)
	GetByTenantIDAndID(ctx context.Context, tenantID TenantID, id string) (Item, error)
	FindByTenantIDAndStatus(ctx context.Context, tenantID uuid.UUID, status model.ItemStatus) ([]Item, error)
	ExistsByTenantIDAndExternalID(ctx context.Context, tenantID uuid.UUID, externalID string) (bool, error)
	CountByTenantIDAndActive(ctx context.Context, tenantID uuid.UUID, active bool) (int64, error)
	DeleteByTenantIDAndID(ctx context.Context, tenantID uuid.UUID, id string) error
	DeleteByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (bool, error)
	DeleteByTenantIDAndStatus(ctx context.Context, tenantID uuid.UUID, status model.ItemStatus) (int64, error)
	FindAllInternal(ctx context.Context) ([]Item, error)
	CountInternal(ctx context.Context) (int64, error)

	// Hand-written. ExistsByTenantIDAndName also derives, but compares the
	// name case-insensitively: the hand-written method wins.
	ExistsByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (bool, error)
	ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error)
	Create(ctx context.Context, item model.Items) (Item, error)
	Archive(ctx context.Context, tenantID uuid.UUID, id string) (bool, error)

	// Generated: each takes a declared search and runs it on the search
	// hatch of the base.
	SearchItems(ctx context.Context, q ItemSearch) (search.Result[Item], error)
	SearchItemSummaries(ctx context.Context, q ItemSummarySearch) (search.Result[itemSummary], error)
	SearchItemsWithOwner(ctx context.Context, q ItemWithOwnerSearch) (search.Result[itemWithOwner], error)
	SearchItemsOfOwner(ctx context.Context, q ItemOfOwnerSearch) (search.Result[itemOfOwner], error)
	SearchManagedLabels(ctx context.Context, q ManagedLabelSearch) (search.Result[Label], error)
	SearchLicensedItems(ctx context.Context, q LicensedItemSearch) (search.Result[itemWithLicense], error)
	SearchCountedItems(ctx context.Context, q CountedItemSearch) (search.Result[itemWithEvents], error)
	SearchProjectsInWorkspace(ctx context.Context, q ProjectInWorkspaceSearch) (search.Result[model.Projects], error)
	SearchProjectsOrgWide(ctx context.Context, q ProjectOrgWideSearch) (search.Result[model.Projects], error)
	// Hand-written: the hand-written method wins, and runs q.Query itself.
	SearchAssignableLabels(ctx context.Context, q AssignableLabelSearch) (search.Result[Label], error)
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
	labels labelMapper
}

// SearchAssignableLabels runs the search on the base hatch, as the
// generated method would: nanostack gen repo checks that it runs q.Query with
// its own ctx and the search's spec.
func (r *itemRepositoryImpl) SearchAssignableLabels(
	ctx context.Context, q AssignableLabelSearch,
) (search.Result[Label], error) {
	return r.search(ctx, assignableLabelSearch, q.Query, r.labels.toLabel)
}

// NewItemRepository returns the item repository on db.
func NewItemRepository(db qrm.DB) ItemRepository {
	return &itemRepositoryImpl{itemsBase: newItemsBase(db)}
}

func (r *itemRepositoryImpl) ExistsByTenantIDAndName(
	ctx context.Context,
	tenantID uuid.UUID,
	name string,
) (bool, error) {
	return r.exists(ctx, r.where.TenantID(tenantID).AND(r.where.NameEqualsIgnoreCase(name)))
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
