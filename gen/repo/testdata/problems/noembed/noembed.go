// Package noembed binds an implementation that does not embed the base its
// derived methods run on.
package noembed

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Items has one derived method.
type Items interface {
	CountByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (int64, error)
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, nanorepo.NoMapper[model.Items])
}

type itemsImpl struct{}
