// Package repository holds the repositories of the db step fixture, on the
// base that nano gen repo writes.
package repository

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"

	"example.com/dbapp/internal/db/gen/app/public/model"
	"example.com/dbapp/internal/db/gen/app/public/table"
)

func _() {
	nanorepo.Base[model.Users](table.Users)
}

// Users reads users.
type Users struct{ usersBase }

// NewUsers returns the users repository on db.
func NewUsers(db qrm.DB) Users { return Users{usersBase: newUsersBase(db)} }

// ByTenant reads the users of a tenant.
func (u Users) ByTenant(ctx context.Context, tenantID string) ([]model.Users, error) {
	return u.findAll(ctx, u.where.TenantID(tenantID))
}

// ByEmail reads the users with an email, in every tenant.
func (u Users) ByEmail(ctx context.Context, email string) ([]model.Users, error) {
	return u.findAll(ctx, u.where.Email(email))
}
