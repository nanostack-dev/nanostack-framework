// Package stale holds the output of a run made before itemsImpl embedded
// extraFinders: stale_repo.gen.go still derives FindByTenantIDAndStatus on
// itemsImpl. nano gen repo never reads a method it generated, so the
// promoted method wins and the derived copy goes.
package stale

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Items has one method, now promoted from extraFinders.
type Items interface {
	FindByTenantIDAndStatus(ctx context.Context, tenantID uuid.UUID, status model.ItemStatus) ([]model.Items, error)
}

func _() {
	nanorepo.Implement[Items, itemsImpl](table.Items, nanorepo.NoMapper[model.Items])
}

type itemsImpl struct {
	itemsBase
	extraFinders
}

type extraFinders struct{}

func (extraFinders) FindByTenantIDAndStatus(
	context.Context, uuid.UUID, model.ItemStatus,
) ([]model.Items, error) {
	return nil, nil
}
