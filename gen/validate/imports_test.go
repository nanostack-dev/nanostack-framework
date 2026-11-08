package genvalidate

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// TestImportNames covers the cases importset solves for this generator: a
// name the package declares, a local of the generated methods, and a user
// package whose name equals a fixed import's.
func TestImportNames(t *testing.T) {
	pkg := types.NewPackage("example.com/api", "api")
	for _, name := range []string{"regexp", "Shape"} {
		pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, name, types.Typ[types.Int]))
	}
	g := &generator{gp: &goPackage{pkg: pkg}, imports: newImports(pkg), renamed: map[string]bool{}}

	g.use(runtimePath)
	g.use("slices")
	if len(g.errs) != 0 {
		t.Fatalf("free names reported: %v", g.errs)
	}
	userValidate := types.NewPackage("example.com/rules/validate", "validate")
	if got := g.qualifier(userValidate); got != "validate2" {
		t.Errorf("qualifier(user validate) = %s, want validate2", got)
	}
	if got := g.qualifier(types.NewPackage("example.com/p", "p")); got != "p2" {
		t.Errorf("qualifier(package p) = %s, want p2: p is a generated local", got)
	}
	if got := g.qualifier(pkg); got != "" {
		t.Errorf("qualifier(self) = %q, want empty", got)
	}

	g.use("regexp")
	g.use("regexp")
	if len(g.errs) != 1 || !strings.Contains(g.errs[0].Error(), "declares regexp") {
		t.Fatalf("errs = %v, want one error naming regexp", g.errs)
	}
}
