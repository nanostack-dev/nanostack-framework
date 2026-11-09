// Package scope holds hand-written queries with and without the tenant
// column; nano gen repo warns on each query of items without tenant_id.
package scope

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Items](table.Items)
	nanorepo.Base[model.AuditEvents](table.AuditEvents)
}

type items struct{ itemsBase }

// Scoped: a predicate on tenant_id.
func (r items) BySKU(ctx context.Context, tenantID uuid.UUID, sku string) (int64, error) {
	return r.count(ctx, r.where.TenantID(tenantID).AND(r.where.Sku(sku)))
}

// Scoped: TenantIDIn counts.
func (r items) ForTenants(ctx context.Context, tenants ...uuid.UUID) ([]model.Items, error) {
	return r.findAll(ctx, r.where.TenantIDIn(tenants...))
}

// Scoped: a reference to the column on r.t.
func (r items) Named(ctx context.Context, tenantID uuid.UUID, name string) ([]model.Items, error) {
	stmt := r.t.SELECT(r.t.AllColumns).WHERE(r.t.TenantID.EQ(postgres.UUID(tenantID)).AND(r.where.Name(name)))
	return r.queryRows(ctx, stmt).Value()
}

// Unscoped: a helper with a predicate on another column.
func (r items) BySKUAnyTenant(ctx context.Context, sku string) (int64, error) {
	return r.count(ctx, r.where.Sku(sku))
}

// Unscoped: a helper with no condition.
func (r items) All(ctx context.Context) ([]model.Items, error) {
	return r.findAll(ctx, nil)
}

// Unscoped: a DELETE built on r.t, in a closure.
func (r items) Purge(ctx context.Context) error {
	run := func() error {
		return r.exec(ctx, r.t.DELETE().WHERE(r.t.DeletedAt.IS_NOT_NULL())).Err()
	}
	return run()
}

// Unscoped but trusted: the name ends in Internal.
func (r items) AllInternal(ctx context.Context) ([]model.Items, error) {
	return r.findAll(ctx, nil)
}

// Unscoped: a function that takes the base.
func countAll(ctx context.Context, b itemsBase) (int64, error) {
	return b.count(ctx, nil)
}

// Not checked: an INSERT on r.t has no condition.
func (r items) Insert(ctx context.Context, row model.Items) error {
	return r.exec(ctx, r.t.INSERT(r.t.AllColumns).MODEL(row)).Err()
}

type audit struct{ auditEventsBase }

// Not checked: audit_events has no tenant column.
func (a audit) All(ctx context.Context) ([]model.AuditEvents, error) {
	return a.findAll(ctx, nil)
}

var _ = countAll
