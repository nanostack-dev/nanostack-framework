package repotest

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// ScopeNames gives the tenant columns of the fixture the short names that a
// derived method uses, as Postgres.ScopeNames declares them in a service.
//
//nolint:gochecknoglobals // fixture
var ScopeNames = map[string]string{"tenant_id": "Tenant", "workspace_id": "Workspace"}

// Project is the domain type of a project.
type Project struct {
	ID   string
	Name string
}

func projectToDomain(row model.Projects) Project { return Project{ID: row.ID, Name: row.Name} }

// ProjectRepository reads and deletes the projects of a tenant, shaped like
// echopoint's webhooks: tenant_id is a hard scope, and workspace_id a
// nullable context scope. Workspace binds no parameter: each derived method
// applies workspacescope.Filter to workspace_id, which keeps the workspace
// of ctx, or every row of the tenant when ctx carries none.
type ProjectRepository interface {
	FindByTenantAndWorkspaceAndID(
		ctx context.Context, tenantID uuid.UUID, id string,
	) (functional.Option[Project], error)
	FindByTenantAndWorkspaceAndName(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
	FindByTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID) ([]Project, error)
	CountByTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID) (int64, error)
	ExistsByTenantAndWorkspaceAndName(ctx context.Context, tenantID uuid.UUID, name string) (bool, error)
	DeleteByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, id string) (bool, error)
	// Internal skips the context scope of a read, whatever workspace ctx carries.
	FindByTenantAndNameInternal(ctx context.Context, tenantID uuid.UUID, name string) ([]Project, error)
}

func _() {
	nanorepo.Implement[ProjectRepository, projectRepositoryImpl](table.Projects, projectToDomain)
}

type projectRepositoryImpl struct{ projectsBase }

// NewProjectRepository returns the project repository on db.
func NewProjectRepository(db qrm.DB) ProjectRepository {
	return &projectRepositoryImpl{projectsBase: newProjectsBase(db)}
}
