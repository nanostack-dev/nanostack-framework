package genrepo

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
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
// end of the file (design S8). The builder alone is counted: G8 adds the
// search method and G12 the API mapper. Shaped in joined.go has the shape
// of design section 4.2, and its builder owns 64 lines.
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

// The forward stub of a search that nano gen repo cannot write yet has its
// scope struct and no builder: the From function that takes the struct
// type-checks, and nothing names a builder that the run will not write.
func TestBuilderForwardStubOfAnUnbuiltSearch(t *testing.T) {
	t.Parallel()
	dir := mustAbs(t, "testdata/builderstub")
	stubs := ForwardStubs(dir)
	stub, ok := stubs[filepath.Join(dir, "stub_repo.gen.go")]
	if !ok {
		t.Fatalf("no stub of stub_repo.gen.go: %v", stubs)
	}
	if !bytes.Contains(stub, []byte("type UnbuiltScope struct")) || bytes.Contains(stub, []byte("type UnbuiltSearch")) {
		t.Errorf("the stub must have the scope struct and no builder:\n%s", stub)
	}
	for name, overlay := range map[string]map[string][]byte{"with": stubs, "without": nil} {
		pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedTypes | packages.NeedName, Dir: dir,
			Overlay: overlay}, ".")
		if err != nil {
			t.Fatal(err)
		}
		if typeChecks := len(pkgs[0].Errors) == 0; typeChecks != (name == "with") {
			t.Errorf("%s the stub: errors %v", name, pkgs[0].Errors)
		}
	}
}

// The stub cache holds a read only while the files it read do not change,
// and it never holds a failed read: in a pipeline run the db step writes a
// table package after the first stub request of the repo package.
func TestBuilderForwardStubCache(t *testing.T) {
	t.Parallel()
	root, err := os.MkdirTemp("testdata", "run-") //nolint:usetesting // must sit inside the module
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	// nano gen db writes the model and enum packages next to the table
	// package; only the table package comes late here.
	public := "internal/repotest/db/shop/public"
	for _, pkg := range []string{"model", "enum"} {
		if err = os.CopyFS(filepath.Join(root, pkg), os.DirFS(filepath.Join(public, pkg))); err != nil {
			t.Fatal(err)
		}
	}
	repoDir, tableDir := filepath.Join(root, "repo"), filepath.Join(root, "table")
	if err = os.Mkdir(repoDir, 0o750); err != nil {
		t.Fatal(err)
	}
	decl := `package repo

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/` + filepath.ToSlash(root) + `/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/` + filepath.ToSlash(root) + `/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)

	nanorepo.Search[model.Items]("Item", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
	)
}
`
	declPath := filepath.Join(repoDir, "a.go")
	if err = os.WriteFile(declPath, []byte(decl), 0o600); err != nil {
		t.Fatal(err)
	}
	stubOf := func() string {
		return string(ForwardStubs(mustAbs(t, repoDir))[mustAbs(t, filepath.Join(repoDir, "a_repo.gen.go"))])
	}
	if stub := stubOf(); strings.Contains(stub, "type ItemSearch") {
		t.Fatalf("a builder before the table package exists:\n%s", stub)
	}
	if err = os.CopyFS(tableDir, os.DirFS(filepath.Join(public, "table"))); err != nil {
		t.Fatal(err)
	}
	if stub := stubOf(); !strings.Contains(stub, "type ItemSearch") {
		t.Fatalf("no builder once the table package exists:\n%s", stub)
	}
	edited := strings.Replace(decl, "nanorepo.Sort(table.Items.Name),",
		"nanorepo.Sort(table.Items.Name),\n\t\tnanorepo.Filter(table.Items.Status, nanorepo.Eq),", 1)
	if err = os.WriteFile(declPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if stub := stubOf(); !strings.Contains(stub, "func (q ItemSearch) Status()") {
		t.Fatalf("the stub does not follow the edited declaration:\n%s", stub)
	}
}

// A read depends on the table packages of the declarations and on the model
// and enum packages next to them: a change in any of them refreshes the
// stub.
func TestBuilderStubDependencies(t *testing.T) {
	t.Parallel()
	dir := mustAbs(t, "testdata/builder")
	result, dirs := readSearches(dir)
	if result == nil {
		t.Fatal("testdata/builder does not read")
	}
	public := mustAbs(t, "internal/repotest/db/shop/public")
	for _, want := range []string{dir, filepath.Join(public, "table"), filepath.Join(public, "model"),
		filepath.Join(public, "enum")} {
		if !slices.Contains(dirs, want) {
			t.Errorf("the read of testdata/builder does not depend on %s: %v", want, dirs)
		}
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

// A key on a nullable column whose value needs a conversion, such as a
// smallint read as *int16, converts the value behind the pointer. No
// fixture table has such a column, so this checks the emitter alone.
func TestNullableKeyConversion(t *testing.T) {
	t.Parallel()
	e := &emitter{imp: importset.New("example.com/p"), self: "example.com/p"}
	k := &sortKey{
		ctor: "NullableIntKey",
		col: &colRef{
			table: &tableBase{pkgPath: "example.com/table", pkgName: "table", goName: "Items"},
			col:   column{field: "Rank"},
		},
		field:   "Rank",
		value:   types.NewPointer(types.Typ[types.Int64]),
		ptrConv: "int64",
	}
	want := "keyset.NullableIntKey(table.Items.Rank, func(r model.Items) *int64 { if r.Rank == nil {\n\t\treturn nil\n\t}\n" +
		"\tv := int64(*r.Rank)\n\treturn &v })"
	if got := e.key(k, "model.Items"); got != want {
		t.Errorf("key =\n%s\nwant\n%s", got, want)
	}
}
