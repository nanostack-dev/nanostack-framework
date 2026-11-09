package genrepo

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// maxSearchLines is the most non-blank lines one fixture search may own in
// its <file>_repo.gen.go, comments included (design S8).
const maxSearchLines = 80

// The builders of testdata/builder are what the golden files hold: one
// scope value (the scope is its type), a scope struct, an empty scope, every
// field type, a key of each kind of column, SortKey API names and NULL
// placement, SortHandWritten, a From function that takes the scope value,
// and a file that declares only a search.
func TestBuilderGolden(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/builder", Scopes: []string{"tenant_id"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 3 {
		t.Fatalf("files: %d, want the base, builder_repo.gen.go and joined_repo.gen.go", len(res.Files))
	}
	for _, f := range res.Files {
		golden(t, filepath.Join("testdata/builder", filepath.Base(f.Path)+".golden"), f.Data)
	}
}

// The package of testdata/builder builds and vets with the generated
// files, and its hand-written code uses the builders.
func TestBuilderBuildsAndVets(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/builder", Scopes: []string{"tenant_id"}})
	if err != nil {
		t.Fatal(err)
	}
	overlay := writeOverlay(t, res.Files)
	for _, cmd := range []string{"build", "vet"} {
		out, cmdErr := exec.CommandContext(t.Context(), "go", cmd, "-overlay="+overlay, "./testdata/builder").
			CombinedOutput()
		if cmdErr != nil {
			t.Errorf("go %s: %v\n%s", cmd, cmdErr, out)
		}
	}
}

// writeOverlay writes files to a temporary directory and returns the go
// command's -overlay file that puts them in place.
func writeOverlay(t *testing.T, files []gen.File) string {
	t.Helper()
	dir := t.TempDir()
	replace := map[string]string{}
	for i, f := range files {
		tmp := filepath.Join(dir, strings.Repeat("f", i+1)+".go")
		if err := os.WriteFile(tmp, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		replace[f.Path] = tmp
	}
	data, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "overlay.json")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Each fixture search owns at most 80 non-blank lines of its generated
// file, from the doc comment of its builder type to the next search or the
// end of the file (design S8).
func TestBuilderLineCount(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/builder/builder_repo.gen.go.golden",
		"testdata/builder/joined_repo.gen.go.golden"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		counts := searchLines(data)
		if len(counts) == 0 {
			t.Fatalf("%s holds no search", path)
		}
		for name, n := range counts {
			if n > maxSearchLines {
				t.Errorf("%s: search %s owns %d non-blank lines, more than %d", path, name, n, maxSearchLines)
			}
		}
	}
}

// searchLines counts the non-blank lines of each search of a generated
// file, from its marker to the next marker or the end of the file.
func searchLines(src []byte) map[string]int {
	counts := map[string]int{}
	current := ""
	for line := range strings.SplitSeq(string(src), "\n") {
		if rest, ok := strings.CutPrefix(line, "// "); ok {
			if name, _, found := strings.Cut(rest, "Search is an immutable search of "); found &&
				!strings.Contains(name, " ") {
				current = name
			}
		}
		if current != "" && strings.TrimSpace(line) != "" {
			counts[current]++
		}
	}
	return counts
}

// The forward stub of testdata/builder declares the builders before nano
// gen repo runs: the package, whose hand-written code names them and whose
// From function takes the generated scope struct, type-checks with it.
func TestBuilderForwardStub(t *testing.T) {
	t.Parallel()
	dir := mustAbs(t, "testdata/builder")
	stubs := ForwardStubs(dir)
	for _, name := range []string{"builder_repo.gen.go", "joined_repo.gen.go"} {
		stub, ok := stubs[filepath.Join(dir, name)]
		if !ok {
			t.Fatalf("no stub of %s: %v", name, stubs)
		}
		if !bytes.HasPrefix(stub, []byte(header)) {
			t.Errorf("the stub of %s has no nano gen repo header", name)
		}
	}
	if !bytes.Contains(stubs[filepath.Join(dir, "joined_repo.gen.go")], []byte("type ItemOfTenantScope struct")) {
		t.Errorf("the stub has no scope struct:\n%s", stubs[filepath.Join(dir, "joined_repo.gen.go")])
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedTypes | packages.NeedName, Dir: dir,
		Overlay: stubs}, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range pkgs[0].Errors {
		t.Errorf("with the stub: %v", e)
	}
}

// Without the stub, the hand-written code of testdata/builder does not
// type-check: the stub test above proves something.
func TestBuilderNeedsTheStub(t *testing.T) {
	t.Parallel()
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedTypes | packages.NeedName,
		Dir: mustAbs(t, "testdata/builder")}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs[0].Errors) == 0 {
		t.Error("testdata/builder type-checks without the generated builders")
	}
}

// -check reports a builder that no longer matches its declaration (O1),
// and the file of a declaring file that no longer declares a search (O2);
// a run rewrites the first and deletes the second.
func TestBuilderCheckAndOrphan(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, "testdata/builder")
	if err := os.Remove(filepath.Join(dir, "use.go")); err != nil {
		t.Fatal(err)
	}
	opts := Options{Dir: dir, Scopes: []string{"tenant_id"}}
	if _, err := Run(opts); err != nil {
		t.Fatal(err)
	}
	opts.Check = true
	if _, err := Run(opts); err != nil {
		t.Fatalf("-check right after a run: %v", err)
	}
	joined := filepath.Join(dir, "joined.go")
	src, err := os.ReadFile(joined)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), "nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc)",
		"nanorepo.DefaultSort(table.Items.Name, nanorepo.Desc)", 1)
	if err = os.WriteFile(joined, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Run(opts); !errors.Is(err, gen.ErrStale) || !strings.Contains(err.Error(), "joined_repo.gen.go") {
		t.Fatalf("-check on a stale builder: %v", err)
	}
	if err = os.WriteFile(joined, []byte("package builder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Run(opts)
	if err == nil || !strings.Contains(err.Error(), "joined_repo.gen.go: orphan") {
		t.Fatalf("-check on the file of a declaration that is gone: %v", err)
	}
	opts.Check = false
	if _, err = Run(opts); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "joined_repo.gen.go")); !os.IsNotExist(err) {
		t.Errorf("the run kept joined_repo.gen.go: %v", err)
	}
}
