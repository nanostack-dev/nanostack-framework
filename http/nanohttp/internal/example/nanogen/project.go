// Package nanogen declares the inputs of the example's code generation that
// nano gen cannot discover: the spec and the package generated from it.
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

// Project is read from source by nano gen; nothing calls it.
var Project = project.Project{ //nolint:gochecknoglobals // a declaration nano gen reads, like nano.Manifest
	API: &project.OpenAPI{
		Spec:   "openapi.yaml",
		Server: project.Package{Dir: "."},
	},
}
