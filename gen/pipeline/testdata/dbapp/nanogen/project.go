// Package nanogen is the project declaration of the db step fixture.
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

var Project = project.Project{
	Postgres: &project.Postgres{Name: "app"},
}
