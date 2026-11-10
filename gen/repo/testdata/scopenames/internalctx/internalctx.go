// Package internalctx names the context scope of a table whose only scope it is,
// and ends the name in Internal: Internal skips only the scopes the name leaves
// out, so a fully named read still needs Present, or is refused.
package internalctx

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Board is the domain type of a board.
type Board struct{ ID string }

func boardToDomain(row model.Boards) Board { return Board{ID: row.ID} }

// Boards has one scope, the context column workspace_id.
type Boards interface {
	// Internal skips nothing here: Workspace is named, so a bare context would read every tenant.
	FindByWorkspaceAndIDInternal(ctx context.Context, id string) (functional.Option[Board], error)
	CountByWorkspaceInternal(ctx context.Context) (int64, error)
	// Internal skips the context scope, and says so.
	FindByIDInternal(ctx context.Context, id string) (functional.Option[Board], error)
}

func _() {
	nanorepo.Implement[Boards, boardsImpl](table.Boards, boardToDomain)
}

type boardsImpl struct{ boardsBase }

// NewBoards returns the boards on db.
func NewBoards(db qrm.DB) Boards { return &boardsImpl{boardsBase: newBoardsBase(db)} }
