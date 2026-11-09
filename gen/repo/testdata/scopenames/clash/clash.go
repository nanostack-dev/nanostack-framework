// Package clash gives a scope column the name of another column's Go field.
package clash

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Project is the domain type of a project.
type Project struct{ ID string }

func projectToDomain(row model.Projects) Project { return Project{ID: row.ID} }

// Projects counts its rows.
type Projects interface {
	Count(ctx context.Context) (int64, error)
}

func _() {
	nanorepo.Implement[Projects, projectsImpl](table.Projects, projectToDomain)
}

type projectsImpl struct{ projectsBase }

// NewProjects returns the projects on db.
func NewProjects(db qrm.DB) Projects { return &projectsImpl{projectsBase: newProjectsBase(db)} }
