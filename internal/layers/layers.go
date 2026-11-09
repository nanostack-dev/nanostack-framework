// Package layers enforces the import layers of plan section 06 on the
// framework's new packages. A package may import only packages of its own
// layer or of the layers below it:
//
//	tooling       gen (its own module, with the nanostack CLI at gen/cmd/nanostack)
//	integrations  http, pg, redis, otel
//	core          core
//
// Below core is the leaf layer of plan section 06: the Leaves, such as
// pkg/functional (Option, Result, Seq). A leaf is its own module with no
// framework dependency, and every layer may import it.
//
// No layered package imports the legacy trees, pkg, modules and app: they
// carry Fx and zerolog, which the layers replace, and plan section 06 lets
// core depend only on the standard library and small libraries. A leaf under
// pkg is not legacy. One listed exception (design B1): an integration package
// may import the pkg primitives of AllowedLegacy, which carry neither Fx nor
// zerolog, until each one moves to its layered home.
//
// The tooling layer is the gen module (gen/go.mod). It requires the root
// module, and the root module never requires it, so golang.org/x/tools and
// the other generator dependencies stay out of the module graph of a runtime
// consumer.
//
// The test reads the import graph with go list, test files included, and
// checks that no layered package depends on Fx or zerolog, directly or
// through an allowed primitive. The depguard rules in .golangci.local.yml
// and gen/.golangci.local.yml give the same rules in editors and in
// golangci-lint.
package layers

import (
	"slices"
	"strings"
)

// Module is the import path of the framework module.
const Module = "github.com/nanostack-dev/nanostack-framework"

// Tooling is the directory of the tooling layer, relative to the repository
// root. It is the nested module Module + "/gen".
const Tooling = "gen"

// Trees are the layered top-level directories, lowest layer first. The last
// one, Tooling, is its own module; the others are in the root module.
var Trees = []string{"core", "http", "pg", "redis", "otel", Tooling} //nolint:gochecknoglobals // read-only table.

// Leaves are the leaf-layer packages, relative to Module, that every layer
// may import (plan section 06, LEAF). Each is its own module and imports no
// other framework package.
var Leaves = []string{"pkg/functional"} //nolint:gochecknoglobals // read-only table.

// AllowedLegacy are the legacy packages, relative to Module, that an
// integration package (layer 2) may import. Each has no Fx and no zerolog in
// its dependencies, and leaves the list when it moves to its layered home:
// the transactor family to db, tracing to otel, health to core, apisec to
// http.
var AllowedLegacy = []string{ //nolint:gochecknoglobals // read-only table.
	"pkg/apisec",
	"pkg/db/pgerr",
	"pkg/db/transactor",
	"pkg/health",
	"pkg/jetx",
	"pkg/tracing",
}

// Fixtures are the packages, relative to Module, that stand in for a
// service package in a generator's tests, with their subpackages. They
// import what generated code imports in a service, such as
// pkg/db/transactor from the base nanostack gen repo writes, so they are not
// layered. Each is internal to its generator: nothing else imports it.
var Fixtures = []string{
	"gen/repo/internal/repotest",
	"gen/repo/internal/torture",
} //nolint:gochecknoglobals // read-only table.

// Layer returns the layer rank of a framework package path, relative to
// Module: 1 for core, 2 for http, pg, redis and otel, 3 for gen. It
// returns 0 for a path outside the layered trees, such as the legacy pkg and
// modules, the Leaves and the Fixtures.
func Layer(rel string) int {
	if slices.ContainsFunc(Fixtures, func(f string) bool { return rel == f || strings.HasPrefix(rel, f+"/") }) {
		return 0
	}
	top, _, _ := strings.Cut(rel, "/")
	switch top {
	case "core":
		return 1
	case "http", "pg", "redis", "otel":
		return 2
	case Tooling:
		return 3
	}
	return 0
}

// Legacy reports whether a framework package path, relative to Module, is in
// a legacy tree (pkg, modules or app) that no layered package may import. A
// leaf and its subpackages are not legacy.
func Legacy(rel string) bool {
	if Leaf(rel) {
		return false
	}
	top, _, _ := strings.Cut(rel, "/")
	switch top {
	case "pkg", "modules", "app":
		return true
	}
	return false
}

// Leaf reports whether a framework package path, relative to Module, is one
// of the Leaves or a subpackage of one.
func Leaf(rel string) bool {
	return slices.ContainsFunc(Leaves, func(leaf string) bool {
		return rel == leaf || strings.HasPrefix(rel, leaf+"/")
	})
}

// MayImport reports whether the framework package from may import the
// framework package to, both relative to Module, and why not.
func MayImport(from, to string) (bool, string) {
	switch {
	case Layer(from) == 0:
		return true, ""
	case Legacy(to) && Layer(from) == 2 && slices.Contains(AllowedLegacy, to):
		return true, ""
	case Legacy(to):
		return false, "a legacy package; move what it needs into the layers"
	case Layer(to) > Layer(from):
		return false, "a higher layer"
	}
	return true, ""
}
