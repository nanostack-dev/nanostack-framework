// Package drift names a column that a migration renamed since the last run:
// the base on disk still has its predicates, so the rename is the first fix.
package drift

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Items names external_ref, the old name of external_id.
type Items interface {
	DeleteByTenantIDAndExternalRef(ctx context.Context, tenantID uuid.UUID, externalRef string) error
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, nanorepo.NoMapper[model.Items])
}

type itemsImpl struct{ itemsBase }
