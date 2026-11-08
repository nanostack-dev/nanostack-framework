// Package project declares the inputs of a module's code generation that
// nano gen cannot discover. A module holds one declaration, a package-level
// variable named Project in nanogen/project.go:
//
//	// Package nanogen declares the inputs of this module's code generation
//	// that cannot be discovered. Run: go tool nano gen
//	package nanogen
//
//	import "github.com/nanostack-dev/nanostack-framework/core/nano/project"
//
//	var Project = project.Project{
//		API: &project.OpenAPI{
//			Spec:   "cmd/http/openapi.yaml",
//			Server: project.Package{Dir: "internal/api"},
//		},
//		Env: "../../.env.example",
//	}
//
// The declaration is plain data, like nano.Manifest: nothing calls it and no
// binary imports the nanogen package. The file carries no build tag, so
// go build, go vet and gopls type-check it, and a misspelled field is a
// compile error. nano gen reads the composite literal from source: every
// value must be a literal or a constant.
//
// Everything else is discovered: //nano:errors catalogues, and the packages
// whose //go:build nanogen file calls nano.Manifest, for config and wire.
// A module without the file still runs the discovered steps.
//
// The types live in the root module, not in the gen module, because the
// root module never requires gen and its own example service declares a
// project too (ADR 0023).
package project

// Project is the declaration of one module's generation inputs. Every path is
// relative to the directory nano gen runs on, the module root by default.
type Project struct {
	// API is the OpenAPI document and the oapi-codegen server package. Nil
	// when the module serves no OpenAPI document; the validate step then
	// does not run.
	API *OpenAPI
	// Env is the .env.example file that nano gen config writes. Empty
	// writes .env.example next to the Config type.
	Env string
}

// OpenAPI is the API contract and the Go package generated from it.
type OpenAPI struct {
	// Spec is the OpenAPI 3.0 or 3.1 document.
	Spec string
	// Server is the package oapi-codegen generates the strict server and
	// the models into. nano gen validate writes validate.gen.go there.
	Server Package
}

// Package is a Go package directory.
type Package struct {
	// Dir is the package directory.
	Dir string
}
