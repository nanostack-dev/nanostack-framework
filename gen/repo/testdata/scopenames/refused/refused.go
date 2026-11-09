// Package refused declares derived names that break the scope rules of
// short names: a context scope that the name leaves out, a context scope
// where the hard scope belongs, and a delete with no escape.
package refused

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Project is the domain type of a project.
type Project struct{ ID string }

func projectToDomain(row model.Projects) Project { return Project{ID: row.ID} }

func projectToRow(p Project) model.Projects { return model.Projects{ID: p.ID} }

// Projects has a hard scope (tenant_id) and a context scope (workspace_id).
type Projects interface {
	// The context scope is left out: a read may end in Internal.
	FindByTenantAndID(ctx context.Context, tenantID uuid.UUID, id string) (functional.Option[Project], error)
	FindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
	// Outside the grammar: the rename names the scopes.
	ListByName(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
	// A delete has no Internal.
	DeleteByTenantAndID(ctx context.Context, tenantID uuid.UUID, id string) error
	DeleteByTenantAndIDInternal(ctx context.Context, tenantID uuid.UUID, id string) error
	// The context scope does not stand for the hard scope.
	FindByWorkspaceAndTenantAndID(ctx context.Context, tenantID uuid.UUID, id string) (functional.Option[Project], error)
	// Neither scope first.
	FindByIDAndTenantAndWorkspace(ctx context.Context, id string, tenantID uuid.UUID) (functional.Option[Project], error)
	// A nullable context scope is not stamped: a Create of it is hand-written.
	CreateInTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID, project Project) error
	// An upsert names the context scope too.
	UpsertByTenantAndID(ctx context.Context, tenantID uuid.UUID, project Project) error
}

func _() {
	nanorepo.Implement[Projects, projectsImpl](table.Projects, projectToDomain, nanorepo.ToRow(projectToRow))
}

type projectsImpl struct{ projectsBase }

// NewProjects returns the projects on db.
func NewProjects(db qrm.DB) Projects { return &projectsImpl{projectsBase: newProjectsBase(db)} }

// Board is the domain type of a board.
type Board struct{ ID string }

func boardToDomain(row model.Boards) Board { return Board{ID: row.ID} }

// Boards has one scope, the context column workspace_id.
type Boards interface {
	FindByID(ctx context.Context, id string) (functional.Option[Board], error)
	FindByIDInternal(ctx context.Context, id string) (functional.Option[Board], error)
}

func _() {
	nanorepo.Implement[Boards, boardsImpl](table.Boards, boardToDomain)
}

type boardsImpl struct{ boardsBase }

// NewBoards returns the boards on db.
func NewBoards(db qrm.DB) Boards { return &boardsImpl{boardsBase: newBoardsBase(db)} }
