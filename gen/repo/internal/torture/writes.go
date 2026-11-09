package torture

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Wides writes a table with two hard scopes (org_id, product_id), the
// context scope workspace_id (stamped on a new row), every column type and
// the columns the database writes. Its key (org_id, product_id, slug) lacks the workspace: the upsert guards it.
type Wides interface {
	FindByOrgAndProductAndWorkspaceAndID(
		ctx context.Context, orgID, productID uuid.UUID, id uuid.UUID,
	) (functional.Option[model.Wide], error)
	CreateInOrgAndProductAndWorkspace(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
	UpdateByOrgAndProductAndWorkspaceAndID(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
	UpdateByOrgAndProductAndWorkspaceAndSlug(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (functional.Option[model.Wide], error)
	UpsertByOrgAndProductAndWorkspaceAndSlug(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
}

// WShelves writes a table whose unique key (product_id, slug) lacks org_id
// and workspace_id: an upsert guards them in its DO UPDATE, so another
// tenant that owns the same product id cannot take the row. Each write
// returns the error only.
type WShelves interface {
	FindByOrgAndProductAndWorkspaceAndID(
		ctx context.Context, orgID, productID uuid.UUID, id string,
	) (functional.Option[model.Wshelf], error)
	CreateInOrgAndProductAndWorkspace(
		ctx context.Context, orgID, productID uuid.UUID, shelf model.Wshelf,
	) error
	UpdateByOrgAndProductAndWorkspaceAndID(
		ctx context.Context, orgID, productID uuid.UUID, shelf model.Wshelf,
	) error
	UpsertByOrgAndProductAndWorkspaceAndSlug(
		ctx context.Context, orgID, productID uuid.UUID, shelf model.Wshelf,
	) error
}

func _() {
	nanorepo.Implement[Wides, widesImpl](table.Wide, nanorepo.NoMapper[model.Wide])
	nanorepo.Implement[WShelves, wShelvesImpl](table.Wshelf, nanorepo.NoMapper[model.Wshelf])
}

type (
	widesImpl    struct{ wideBase }
	wShelvesImpl struct{ wshelfBase }
)

// NewWides returns the wides on db.
func NewWides(db qrm.DB) Wides { return &widesImpl{wideBase: newWideBase(db)} }

// NewWShelves returns the wshelves on db.
func NewWShelves(db qrm.DB) WShelves { return &wShelvesImpl{wshelfBase: newWshelfBase(db)} }
