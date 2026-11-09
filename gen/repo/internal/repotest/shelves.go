package repotest

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// shelves has two hard scopes, tenant_id and store_id, and the context
// scope workspace_id. A derived write names all three, so none comes from
// the value. Its key (store_id, name) holds store_id only: the upsert guards
// tenant_id and workspace_id in its DO UPDATE.

// ShelfRepository writes the shelves of a store of a tenant, with the
// NoMapper model as its value.
type ShelfRepository interface {
	CreateInTenantAndStoreIDAndWorkspace(
		ctx context.Context, tenantID uuid.UUID, storeID string, shelf model.Shelves,
	) (model.Shelves, error)
	UpdateByTenantAndStoreIDAndWorkspaceAndID(
		ctx context.Context, tenantID uuid.UUID, storeID string, shelf model.Shelves,
	) (model.Shelves, error)
	UpsertByTenantAndStoreIDAndWorkspaceAndName(
		ctx context.Context, tenantID uuid.UUID, storeID string, shelf model.Shelves,
	) (model.Shelves, error)
}

func _() {
	nanorepo.Implement[ShelfRepository, shelfRepositoryImpl](table.Shelves, nanorepo.NoMapper[model.Shelves])
}

type shelfRepositoryImpl struct{ shelvesBase }

// NewShelfRepository returns the shelf repository on db.
func NewShelfRepository(db qrm.DB) ShelfRepository {
	return &shelfRepositoryImpl{shelvesBase: newShelvesBase(db)}
}
