package genrepo

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest"
)

var update = flag.Bool("update", false, "rewrite the golden files of the tests")

const fixture = "internal/repotest"

// golden compares got with the file at path, or rewrites it with -update.
func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./repo/ -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs; run go test ./repo/ -update and review the diff\ngot:\n%s", path, got)
	}
}

// The committed base of the fixture, which its integration tests run on
// PostgreSQL, is what the generator writes, its clean repositories get no
// warning, and its exemption file is testdata/exemptions.golden.
func TestFixtureBaseIsCurrent(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: fixture, Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("warnings on the fixture: %v", res.Warnings)
	}
	if res.Files[0].Path != mustAbs(t, filepath.Join(fixture, FileName)) {
		t.Errorf("path %s", res.Files[0].Path)
	}
	for _, f := range res.Files {
		golden(t, f.Path, f.Data)
	}
	// The exemption file lists the shared rows of tenant.go (ScopeNull,
	// ScopeOrNull), which a review must see.
	lines := make([]string, len(res.Exemptions))
	for i, e := range res.Exemptions {
		lines[i] = e.String()
	}
	golden(t, "testdata/exemptions.golden", ExemptionsData(lines))
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// The base gives Contains and EqualsIgnoreCase to the columns whose
// catalogue type is text (text, varchar here), never to a column that the
// model reads as a string but that is not text (inet, jsonb), and the
// Contains predicates call searchexec.Contains.
func TestContainsOnTextColumnsOnly(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: fixture, Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Files[0].Data)
	for _, want := range []string{
		"func (auditEventsWhere) ActionContains(term string) postgres.BoolExpression {\n" +
			"\treturn searchexec.Contains(term, table.AuditEvents.Action)\n}",
		"func (auditEventsWhere) NoteContains(term string) postgres.BoolExpression {\n" +
			"\treturn searchexec.Contains(term, table.AuditEvents.Note)\n}",
		"func (auditEventsWhere) NoteEqualsIgnoreCase(",
		"// No equality predicate on client, payload: compare on b.t.",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the base has no %q", want)
		}
	}
	for _, unwanted := range []string{
		"ClientContains", "ClientEqualsIgnoreCase", "PayloadContains", "PayloadEqualsIgnoreCase",
		"func (auditEventsWhere) Client(", "func (auditEventsWhere) ClientIn(",
		"func (auditEventsWhere) Payload(", "func (auditEventsWhere) PayloadIn(",
		"ItemIDContains", ".LIKE(", "nanoContainsPattern",
	} {
		if strings.Contains(src, unwanted) {
			t.Errorf("the base has %q", unwanted)
		}
	}
}

// A base on a view without ColumnTypes generates: its string columns keep
// the equality predicates, get no Contains or EqualsIgnoreCase, and the doc
// comment names them.
func TestViewBaseWithoutColumnTypes(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/viewbase"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/viewbase/repo_base.gen.go.golden", res.Files[0].Data)
	src := string(res.Files[0].Data)
	for _, want := range []string{
		"func (auditEventsWhere) Action(v string)",
		"// audit_events is a view, and nano gen db writes no ColumnTypes on a view yet:\n" +
			"// no Contains or EqualsIgnoreCase on action. Call searchexec.Contains on b.t.",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the base has no %q", want)
		}
	}
	if strings.Contains(src, "ActionContains") || strings.Contains(src, "ActionEqualsIgnoreCase") {
		t.Error("the base of a view without ColumnTypes has a Contains or EqualsIgnoreCase predicate")
	}
}

// Two declarations on one table share its base, a method value on a type
// without fields is a mapper, and a mapper of another package is imported.
func TestSharedBase(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/shared"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/shared/repo_base.gen.go.golden", res.Files[0].Data)
	if n := bytes.Count(res.Files[0].Data, []byte("type itemsBase struct")); n != 1 {
		t.Errorf("itemsBase declared %d times, want once", n)
	}
}

// A base where every table has a mapper does not name the row type, so it
// must not import the model package: an unused import does not compile.
func TestBaseWithOnlyMappedTables(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/allmapped"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatal("no file generated")
	}
	if src := string(res.Files[0].Data); strings.Contains(src, "/public/model\"") {
		t.Error("the base imports the model package, which it does not use")
	}
}

// A binding whose derived methods return no row does not name the row type
// in its derived file, so that file must not import the row's package.
func TestDerivedFileWithoutRows(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/scalaronly"})
	if err != nil {
		t.Fatal(err)
	}
	var derived string
	for _, f := range res.Files {
		if filepath.Base(f.Path) == "scalaronly_repo.gen.go" {
			derived = string(f.Data)
		}
	}
	if derived == "" {
		t.Fatal("no derived file generated")
	}
	if strings.Contains(derived, "/public/model\"") {
		t.Error("the derived file imports the model package, which it does not use")
	}
}

// report renders a run as Main prints it: the warnings, then the errors.
func report(warnings []*gen.Error, err error) []byte {
	var b bytes.Buffer
	for _, w := range warnings {
		b.WriteString("warning: " + w.Error() + "\n")
	}
	if err != nil {
		gen.Report(&b, "repo", err)
	}
	// A compiler message names a declaration by its absolute position, which
	// moves with any edit of that file.
	return declaredAt.ReplaceAll(b.Bytes(), []byte("(declared at <position>)"))
}

var declaredAt = regexp.MustCompile(`\(declared at [^)]*\)`)

// Each package under testdata/problems prints the errors and warnings of
// want.txt: one per rule, all in one run, each with a fix. A package with
// an openapi.yaml is checked against it. A package whose
// declarations resolve also prints the -explain of want-explain.txt.
func TestProblems(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob("testdata/problems/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			opts := Options{Dir: dir, Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes}
			if _, statErr := os.Stat(filepath.Join(dir, "openapi.yaml")); statErr == nil {
				opts = apiOptions(dir)
			}
			res, genErr := Generate(opts)
			var warnings []*gen.Error
			if genErr != nil {
				gentest.Problems(t, genErr)
				if gentest.Environment(genErr) {
					t.Fatalf("an environment error: %v", genErr)
				}
			} else {
				warnings = res.Warnings
			}
			for _, w := range warnings {
				if w.Fix == "" {
					t.Errorf("warning %q has no fix", w.Msg)
				}
			}
			golden(t, filepath.Join(dir, "want.txt"), report(warnings, genErr))
			explainGolden(t, filepath.Join(dir, "want-explain.txt"), res)
		})
	}
}

// explainGolden compares the -explain of res with path, which exists only
// when the declarations resolved.
func explainGolden(t *testing.T, path string, res *Result) {
	t.Helper()
	if res == nil || len(res.Explain)+len(res.Searches) == 0 {
		if _, err := os.Stat(path); err == nil && !*update {
			t.Errorf("%s exists, but the run explains nothing", path)
		}
		return
	}
	var b bytes.Buffer
	WriteExplain(&b, res.Explain)
	WriteSearchExplain(&b, res.Searches)
	golden(t, path, b.Bytes())
}

// Main writes the base, prints the warnings and exits 0; -check exits 1 on
// a stale base and names the fix; a bad flag exits 2.
func TestMain_(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, "testdata/problems/scope")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"-dir", dir, "-scopes", "tenant_id, other"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nano gen repo: wrote ") ||
		!strings.HasPrefix(stderr.String(), "warning: scope.go:") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Main([]string{"-dir", dir, "-check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("-check right after a run: exit %d, stderr:\n%s", code, stderr.String())
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(header+"package scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := Main([]string{"-dir", dir, "-check"}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "run nano gen repo") {
		t.Fatalf("-check on a stale base: exit %d, stderr:\n%s", code, stderr.String())
	}
	if code := Main([]string{"-nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad flag: exit %d", code)
	}
}

// copyPackage copies the Go files of a test package into a new directory
// inside the module, removed at the end of the test.
func copyPackage(t *testing.T, from string) string {
	t.Helper()
	dir, err := os.MkdirTemp("testdata", "run-") //nolint:usetesting // must sit inside the module
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(from, e.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	return dir
}

// Generation is deterministic: two runs give the same bytes.
func TestDeterministic(t *testing.T) {
	t.Parallel()
	first, err := Generate(Options{Dir: "testdata/shared"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(Options{Dir: "testdata/shared"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(first.Files, again.Files, func(a, b gen.File) bool {
		return a.Path == b.Path && bytes.Equal(a.Data, b.Data)
	}) {
		t.Fatal("two runs differ")
	}
}

// An overlay replaces files in every load, as the pipeline's -check does:
// a declaration that exists only in the overlay is read.
func TestOverlay(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, "testdata/problems/none")
	src := []byte(`package none

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() { nanorepo.Base[model.Tenants](table.Tenants) }
`)
	res, err := Generate(Options{Dir: dir, Overlay: map[string][]byte{mustAbs(t, filepath.Join(dir, "none.go")): src}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(res.Files[0].Data, []byte("type tenantsBase struct")) {
		t.Fatalf("no tenantsBase in:\n%s", res.Files[0].Data)
	}
	if _, err = Generate(Options{Dir: dir, Overlay: map[string][]byte{"relative.go": src}}); err == nil {
		t.Fatal("a relative overlay path was accepted")
	}
}
