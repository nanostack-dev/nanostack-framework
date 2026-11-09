// Package fields has no Postgres.ScopeNames: a context scope column is still
// required in a derived name, by its Go field, which takes a parameter.
package fields

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Board is the domain type of a board.
type Board struct{ ID string }

func boardToDomain(row model.Boards) Board { return Board{ID: row.ID} }

// Boards has one scope, the context column workspace_id, NOT NULL.
type Boards interface {
	FindByWorkspaceIDAndID(ctx context.Context, workspaceID, id string) (functional.Option[Board], error)
	FindByID(ctx context.Context, id string) (functional.Option[Board], error)
	FindByIDInternal(ctx context.Context, id string) (functional.Option[Board], error)
}

func _() {
	nanorepo.Implement[Boards, boardsImpl](table.Boards, boardToDomain)
}

type boardsImpl struct{ boardsBase }

// NewBoards returns the boards on db.
func NewBoards(db qrm.DB) Boards { return &boardsImpl{boardsBase: newBoardsBase(db)} }

// Project is the domain type of a project.
type Project struct{ ID string }

func projectToDomain(row model.Projects) Project { return Project{ID: row.ID} }

// Projects has a nullable context column, which a parameter cannot compare.
type Projects interface {
	FindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
}

func _() {
	nanorepo.Implement[Projects, projectsImpl](table.Projects, projectToDomain)
}

type projectsImpl struct{ projectsBase }

// NewProjects returns the projects on db.
func NewProjects(db qrm.DB) Projects { return &projectsImpl{projectsBase: newProjectsBase(db)} }
