package gopkg_test

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
	packages.NeedTypesInfo | packages.NeedImports

// writeModule writes a module with a root package that imports a local
// package, which an overlay can change, and the standard library.
func writeModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.24\n",
		"dep/dep.go":   "package dep\n\nimport \"strings\"\n\nfunc New() *strings.Builder { return &strings.Builder{} }\n",
		"root/root.go": "package root\n\nimport (\n\t\"strings\"\n\n\t\"example.com/m/dep\"\n)\n\nvar B *strings.Builder = dep.New()\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func load(t *testing.T, loadFn func(*packages.Config, ...string) ([]*packages.Package, error), dir string,
	overlay map[string][]byte,
) *packages.Package {
	t.Helper()
	pkgs, err := loadFn(&packages.Config{
		Mode: loadMode, Dir: filepath.Join(dir, "root"), Fset: token.NewFileSet(), Overlay: overlay,
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("loaded %d packages, want 1", len(pkgs))
	}
	return pkgs[0]
}

// positioned lists the errors that carry a position, sorted. The go
// command's compile report is left out: it names a temporary overlay file
// that differs on every load.
func positioned(errs []packages.Error) []string {
	var out []string
	for _, e := range errs {
		if e.Kind != packages.ListError {
			out = append(out, e.Pos+" "+e.Msg)
		}
	}
	slices.Sort(out)
	return out
}

// TestLoadOverlayMatchesPackagesLoad checks that Load sees overlaid files in
// the root and in a dependency, links the types of every import to one
// object, and reports the errors go/packages reports.
func TestLoadOverlayMatchesPackagesLoad(t *testing.T) {
	dir := writeModule(t)
	overlay := map[string][]byte{
		// A dependency changed only in the overlay: its export data must
		// come from the overlaid source.
		filepath.Join(dir, "dep", "dep.go"): []byte("package dep\n\nimport \"strings\"\n\n" +
			"func New() *strings.Builder { return &strings.Builder{} }\n\nfunc Added() int { return 1 }\n"),
		// A root file that exists only in the overlay, with a type error.
		filepath.Join(dir, "root", "extra.go"): []byte("package root\n\nimport \"example.com/m/dep\"\n\n" +
			"var N int = dep.Added()\n\nvar Bad string = dep.Added()\n"),
	}

	got := load(t, gopkg.Load, dir, overlay)
	want := load(t, packages.Load, dir, overlay)

	if g, w := positioned(got.Errors), positioned(want.Errors); !slices.Equal(g, w) || len(g) != 1 {
		t.Errorf("errors = %q, want %q (one type error)", g, w)
	}
	if g, w := len(got.Errors), len(want.Errors); g != w {
		t.Errorf("%d errors, want %d with the compile report", g, w)
	}
	if !got.IllTyped {
		t.Error("a package with a type error must be IllTyped")
	}
	if len(got.Syntax) != 2 {
		t.Errorf("parsed %d files, want 2 with the overlay-only one", len(got.Syntax))
	}
	if got.Types.Scope().Lookup("N") == nil {
		t.Error("the overlay-only file is not type-checked")
	}

	// The root's strings.Builder and the one dep.New returns are one type.
	b := got.Types.Scope().Lookup("B").Type()
	newFn, ok := got.Imports["example.com/m/dep"].Types.Scope().Lookup("New").Type().(*types.Signature)
	if !ok || !types.Identical(b, newFn.Results().At(0).Type()) {
		t.Error("strings.Builder from the root and from dep are different types")
	}
}

// TestLoadWithoutOverlay checks the plain path, which goes to packages.Load.
func TestLoadWithoutOverlay(t *testing.T) {
	got := load(t, gopkg.Load, writeModule(t), nil)
	if len(got.Errors) != 0 || got.Types.Scope().Lookup("B") == nil {
		t.Errorf("errors = %v, B = %v", got.Errors, got.Types.Scope().Lookup("B"))
	}
}
