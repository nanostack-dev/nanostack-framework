package refused

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// The write scope rule: no scope column may come from the value.

// NoteWrites names a scope wrong in each method.
type NoteWrites interface {
	// The context scope by its Go field would take the workspace from a
	// parameter instead of stamping it.
	CreateInTenantAndWorkspaceID(ctx context.Context, tenantID uuid.UUID, workspaceID string, note model.Notes) error
	// No tenant: it would come from the value.
	UpdateByWorkspaceIDAndID(ctx context.Context, workspaceID string, note model.Notes) error
}

// ShelfWrites leaves out the second hard scope, store_id.
type ShelfWrites interface {
	CreateInTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID, shelf model.Shelves) error
	UpsertByTenantAndWorkspaceAndName(ctx context.Context, tenantID uuid.UUID, shelf model.Shelves) error
}

// LabelWrites is on a table whose only scope, tenant_id, is nullable.
type LabelWrites interface {
	Create(ctx context.Context, label model.Labels) error
	UpdateByID(ctx context.Context, label model.Labels) error
}

// BoardWrites is on a table whose only scope is the context one.
type BoardWrites interface {
	UpdateByWorkspaceAndID(ctx context.Context, board Board) error
}

// LabelUseWrites is on a table with no scope column.
type LabelUseWrites interface {
	Create(ctx context.Context, use model.LabelUses) error
}

func _() {
	nanorepo.Implement[NoteWrites, noteWritesImpl](table.Notes, nanorepo.NoMapper[model.Notes])
	nanorepo.Implement[ShelfWrites, shelfWritesImpl](table.Shelves, nanorepo.NoMapper[model.Shelves])
	nanorepo.Implement[LabelWrites, labelWritesImpl](table.Labels, nanorepo.NoMapper[model.Labels])
	nanorepo.Implement[BoardWrites, boardWritesImpl](table.Boards, boardToDomain,
		nanorepo.ToRow(boardToRow))
	nanorepo.Implement[LabelUseWrites, labelUseWritesImpl](table.LabelUses, nanorepo.NoMapper[model.LabelUses])
}

type (
	noteWritesImpl     struct{ notesBase }
	shelfWritesImpl    struct{ shelvesBase }
	labelWritesImpl    struct{ labelsBase }
	boardWritesImpl    struct{ boardsBase }
	labelUseWritesImpl struct{ labelUsesBase }
)

func boardToRow(b Board) model.Boards { return model.Boards{ID: b.ID} }
