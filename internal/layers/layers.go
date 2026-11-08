// Package layers enforces the import layers of plan section 06 on the
// framework's new packages. A package may import only packages of its own
// layer or of the layers below it:
//
//	tooling       gen, cmd
//	integrations  http
//	core          core
//
// No layered package imports the legacy trees, pkg, modules and app: they
// carry Fx and zerolog, which the layers replace, and plan section 06 lets
// core depend only on the standard library and small libraries.
//
// The test reads the import graph with go list, test files included. The
// depguard rules in .golangci.local.yml give the same rules in editors and in
// golangci-lint.
package layers

import "strings"

// Module is the import path of the framework module.
const Module = "github.com/nanostack-dev/nanostack-framework"

// Layer returns the layer rank of a framework package path, relative to
// Module: 1 for core, 2 for http, 3 for gen and cmd. It returns 0 for a path
// outside the layered trees, such as the legacy pkg and modules.
func Layer(rel string) int {
	top, _, _ := strings.Cut(rel, "/")
	switch top {
	case "core":
		return 1
	case "http":
		return 2
	case "gen", "cmd":
		return 3
	}
	return 0
}

// Legacy reports whether a framework package path, relative to Module, is in
// a legacy tree (pkg, modules or app) that no layered package may import.
func Legacy(rel string) bool {
	top, _, _ := strings.Cut(rel, "/")
	switch top {
	case "pkg", "modules", "app":
		return true
	}
	return false
}
