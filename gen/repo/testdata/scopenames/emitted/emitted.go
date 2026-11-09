// Package emitted declares derived methods that name a scope column by its
// short name (Postgres.ScopeNames): the short name of a context scope binds
// no parameter and applies the registered function.
package emitted

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

// Projects has a hard scope (tenant_id) and a context scope (workspace_id).
type Projects interface {
	// The short names: Tenant takes a parameter, Workspace takes none.
	FindByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, id string) (functional.Option[Project], error)
	FindByTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID) ([]Project, error)
	CountByTenantAndWorkspaceAndName(ctx context.Context, tenantID uuid.UUID, name string) (int64, error)
	DeleteByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, id string) error
	// The Go field names of the columns derive as before.
	FindByTenantIDAndWorkspaceAndName(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
	// The field of a nullable context column keeps its parameter and plain equality.
	FindByTenantAndWorkspaceIDAndName(ctx context.Context, tenantID uuid.UUID, workspaceID, name string) ([]Project, error)
	// Internal skips the context scope of a read.
	FindByTenantAndIDInternal(ctx context.Context, tenantID uuid.UUID, id string) (functional.Option[Project], error)
}

func _() {
	nanorepo.Implement[Projects, projectsImpl](table.Projects, projectToDomain)
}

type projectsImpl struct{ projectsBase }

// NewProjects returns the projects on db.
func NewProjects(db qrm.DB) Projects { return &projectsImpl{projectsBase: newProjectsBase(db)} }

// Board is the domain type of a board.
type Board struct{ ID string }

func boardToDomain(row model.Boards) Board { return Board{ID: row.ID} }

// Boards has one scope, the context column workspace_id, NOT NULL: its short
// name starts the name, and its field name keeps a parameter.
type Boards interface {
	FindByWorkspaceAndID(ctx context.Context, id string) (functional.Option[Board], error)
	FindByWorkspaceIDAndID(ctx context.Context, workspaceID, id string) (functional.Option[Board], error)
	CountByWorkspace(ctx context.Context) (int64, error)
}

func _() {
	nanorepo.Implement[Boards, boardsImpl](table.Boards, boardToDomain)
}

type boardsImpl struct{ boardsBase }

// NewBoards returns the boards on db.
func NewBoards(db qrm.DB) Boards { return &boardsImpl{boardsBase: newBoardsBase(db)} }
