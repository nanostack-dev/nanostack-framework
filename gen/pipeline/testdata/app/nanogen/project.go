// Package nanogen is the project declaration of the pipeline fixture.
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

// spec is a constant: the reader accepts constants as well as literals.
const spec = "api/openapi.yaml"

var Project = project.Project{
	API: &project.OpenAPI{
		Spec:   spec,
		Server: project.Package{Dir: "api"},
	},
}
