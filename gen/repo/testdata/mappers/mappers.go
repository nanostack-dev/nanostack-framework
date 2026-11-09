// Package mappers holds the mappers the test packages of nano gen repo
// import from another package.
package mappers

import (
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
)

// Item is a domain item.
type Item struct {
	ID   string
	Name string
}

// Tenant is a domain tenant.
type Tenant struct {
	Name string
}

// TenantToDomain maps a tenant row.
func TenantToDomain(row model.Tenants) Tenant { return Tenant{Name: row.Name} }

// ItemToDomain maps an item row.
func ItemToDomain(row model.Items) Item { return Item{ID: row.ID, Name: row.Name} }

// ItemToDomainVar is a mapper variable of this package.
var ItemToDomainVar = ItemToDomain
