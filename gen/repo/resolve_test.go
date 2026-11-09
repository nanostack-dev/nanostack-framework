package genrepo

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest"
)

// A method promoted from a newly embedded field wins over the derived copy
// a stale output still declares (design D10): nano gen repo never reads a
// method it generated.
func TestStaleOutputNeverHidesAPromotedMethod(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/stale", Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	WriteExplain(&b, res.Explain)
	if !strings.Contains(b.String(), "FindByTenantIDAndStatus  hand-written: promoted from the embedded extraFinders") {
		t.Fatalf("explain:\n%s", b.String())
	}
	for _, f := range res.Files {
		if bytes.Contains(f.Data, []byte(") FindByTenantIDAndStatus(")) {
			t.Fatalf("%s still declares the derived copy:\n%s", f.Path, f.Data)
		}
	}
}

// A hand-written method deleted without a rerun breaks the build at the
// var _ line of the derived file (design D14), and the next run fails at the
// interface method, with the signature to write.
func TestBackstop(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, fixture)
	path := filepath.Join(dir, "repository.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(src, []byte("func (r *itemRepositoryImpl) ListActive("))
	end := bytes.Index(src[start:], []byte("\n}\n"))
	if start < 0 || end < 0 {
		t.Fatal("no ListActive in the fixture")
	}
	if err = os.WriteFile(path, append(src[:start:start], src[start+end+3:]...), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedTypes | packages.NeedName, Dir: dir}, ".")
	if err != nil {
		t.Fatal(err)
	}
	var build []string
	for _, e := range pkgs[0].Errors {
		build = append(build, e.Error())
	}
	if !strings.Contains(strings.Join(build, "\n"), "repository_repo.gen.go") ||
		!strings.Contains(strings.Join(build, "\n"), "(missing method ListActive)") {
		t.Fatalf("the build does not stop at the var _ line: %v", build)
	}
	_, err = Generate(Options{Dir: dir, Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes})
	want := "ItemRepository.ListActive: not implemented: no hand-written method on *itemRepositoryImpl, and the " +
		"name does not derive: it does not start with FindBy"
	if err == nil || !strings.Contains(err.Error(), want) ||
		!strings.Contains(
			err.Error(),
			"func (r *itemRepositoryImpl) ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error)",
		) {
		t.Fatalf("got %v, want it to contain %q", err, want)
	}
}

// fixtureScopeFlags are the -scopes and -context-scopes flags that declare
// the tenant columns of the fixture, repotest.Scopes and
// repotest.ContextScopes.
func fixtureScopeFlags() []string {
	ctx := make([]string, 0, len(repotest.ContextScopes))
	for col, fn := range repotest.ContextScopes {
		ctx = append(ctx, col+"="+fn)
	}
	slices.Sort(ctx)
	return []string{"-scopes", strings.Join(repotest.Scopes, ","), "-context-scopes", strings.Join(ctx, ",")}
}

// -check names a stale derived file and an orphan; a run rewrites the one
// and deletes the other.
func TestCheckAndOrphans(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, fixture)
	derived := filepath.Join(dir, "repository"+derivedSuffix)
	data, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(derived, bytes.Replace(data, []byte("derived from its name"), []byte("edited"), 1),
		0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "gone"+derivedSuffix)
	if err = os.WriteFile(orphan, []byte(header+"package repotest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := append([]string{"-dir", dir}, fixtureScopeFlags()...)
	if code := Main(append(args, "-check"), &stdout, &stderr); code != 1 {
		t.Fatalf("-check: exit %d, stderr:\n%s", code, stderr.String())
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/check.golden", bytes.ReplaceAll(stderr.Bytes(), []byte(abs), []byte("<dir>")))
	stdout.Reset()
	stderr.Reset()
	if code := Main(args, &stdout, &stderr); code != 0 {
		t.Fatalf("run: exit %d, stderr:\n%s", code, stderr.String())
	}
	want := "nano gen repo: wrote " + mustAbs(t, derived) + "\nnano gen repo: deleted " + mustAbs(t, orphan) + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan is still there: %v", err)
	}
	stderr.Reset()
	if code := Main(append(args, "-check"), &stdout, &stderr); code != 0 {
		t.Fatalf("-check after the run: exit %d, stderr:\n%s", code, stderr.String())
	}
}

// -explain prints the resolution of the interfaces named, writes nothing,
// and exits 1 when no declaration binds them; names without -explain are bad
// usage.
func TestMainExplain(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := Main(append(append([]string{"-dir", fixture}, fixtureScopeFlags()...), "-explain", "AuditLog"),
		&stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if want := "AuditLog on *auditLogImpl (table audit_events)\n  Append  hand-written repository.go:"; !strings.HasPrefix(
		stdout.String(),
		want,
	) {
		t.Fatalf("stdout:\n%s", stdout.String())
	}
	stdout.Reset()
	if code := Main([]string{"-dir", fixture, "-explain", "Nope"}, &stdout, &stderr); code != 1 || stdout.Len() > 0 {
		t.Fatalf("an unknown interface: exit %d, stdout:\n%s", code, stdout.String())
	}
	if code := Main([]string{"-dir", fixture, "AuditLog"}, &stdout, &stderr); code != 2 {
		t.Fatalf("a name without -explain: exit %d", code)
	}
}

// The forward stub of a derived file declares every method of the bound
// interface that has no hand-written method, the one the interface just
// gained included, so the package type-checks before nano gen repo runs
// again (design D27).
func TestForwardStub(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, fixture)
	path := filepath.Join(dir, "repository.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src = bytes.Replace(src, []byte("type ItemRepository interface {\n"),
		[]byte("type ItemRepository interface {\n\tFindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, "+
			"name string) ([]Item, error)\n"), 1)
	if err = os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	stubs := ForwardStubs(mustAbs(t, dir))
	derived := mustAbs(t, filepath.Join(dir, "repository"+derivedSuffix))
	stub, ok := stubs[derived]
	_, search := stubs[mustAbs(t, filepath.Join(dir, "search"+derivedSuffix))]
	_, row := stubs[mustAbs(t, filepath.Join(dir, "row"+derivedSuffix))]
	_, tenant := stubs[mustAbs(t, filepath.Join(dir, "tenant"+derivedSuffix))]
	if len(stubs) != 4 || !ok || !search || !row || !tenant {
		t.Fatalf("stubs for %d files, want %s and the stubs of search.go, row.go and tenant.go", len(stubs), derived)
	}
	for _, want := range []string{"func (r *itemRepositoryImpl) FindByTenantIDAndName(", "FindAllInternal(",
		`import uuid "github.com/google/uuid"`} {
		if !bytes.Contains(stub, []byte(want)) {
			t.Errorf("the stub has no %q:\n%s", want, stub)
		}
	}
	for _, hand := range []string{") ListActive(", ") Create(", "var _"} {
		if bytes.Contains(stub, []byte(hand)) {
			t.Errorf("the stub declares %q, which the package has:\n%s", hand, stub)
		}
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

// A method the last run derived that is now hand-written leaves the
// forward stub, which replaces the last output: the body-stripped stub of
// that output would declare the method twice. A method of an interface
// embedded from another package, which parsing cannot list, is taken from
// the last output.
func TestForwardStubReplacesTheLastOutput(t *testing.T) {
	t.Parallel()
	dir := copyPackage(t, fixture)
	path := filepath.Join(dir, "repository.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src = bytes.Replace(src, []byte("type ItemRepository interface {\n"),
		[]byte("type ItemRepository interface {\n\tdriver.Pinger\n"), 1)
	src = bytes.Replace(src, []byte("import (\n"), []byte("import (\n\t\"database/sql/driver\"\n"), 1)
	if err = os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	derived := filepath.Join(dir, "repository"+derivedSuffix)
	last, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	last = append(last, []byte("\nfunc (r *itemRepositoryImpl) Ping(ctx context.Context) error { return nil }\n")...)
	if err = os.WriteFile(derived, last, 0o600); err != nil {
		t.Fatal(err)
	}
	hand := "package repotest\n\nimport (\n\t\"context\"\n\n\t\"github.com/google/uuid\"\n)\n\n" +
		"func (r *itemRepositoryImpl) DeleteByTenantIDAndID(ctx context.Context, tenantID uuid.UUID, id string) error {\n" +
		"\t_, err := r.deleteWhere(ctx, r.where.TenantID(tenantID).AND(r.where.ID(id)))\n\treturn err\n}\n"
	if err = os.WriteFile(filepath.Join(dir, "hand.go"), []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	stubs := ForwardStubs(mustAbs(t, dir))
	stub := stubs[mustAbs(t, derived)]
	if !bytes.Contains(stub, []byte(") Ping(ctx context.Context) error")) {
		t.Errorf("the stub has no Ping of the last output:\n%s", stub)
	}
	if bytes.Contains(stub, []byte(") DeleteByTenantIDAndID(")) {
		t.Errorf("the stub declares the hand-written DeleteByTenantIDAndID:\n%s", stub)
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

// The pipeline plans a derived file for every file that calls
// nanorepo.Implement, whatever its type arguments: an interface of another
// package binds as one of the declaring package does. A file that declares
// only searches gets one for their builders. A file with only nanorepo.Base
// gets none.
func TestPlanned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string]string{
		"local.go": "package p\n\nimport \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n\n" +
			"func _() { nanorepo.Implement[Repo, impl](table.X, toDomain) }\n",
		"qualified.go": "package p\n\nimport nr \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n\n" +
			"func _() { nr.Implement[domain.Repo, impl](table.X, toDomain) }\n",
		"generic.go": "package p\n\nimport \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n\n" +
			"func _() { nanorepo.Implement[Repo[model.X], impl](table.X, toDomain) }\n",
		"base.go": "package p\n\nimport \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n\n" +
			"func _() { nanorepo.Base[model.X](table.X) }\n",
		"search.go": "package p\n\nimport \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n\n" +
			"func _() { nanorepo.Search[model.X](\"X\", table.X) }\n",
	}
	var paths []string
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	got := Planned(dir, paths)
	want := []string{FileName, "generic_repo.gen.go", "local_repo.gen.go", "qualified_repo.gen.go",
		"search_repo.gen.go"}
	names := make([]string, len(got))
	for i, p := range got {
		names[i] = filepath.Base(p)
	}
	slices.Sort(names[1:])
	if !slices.Equal(names, want) {
		t.Fatalf("Planned = %v, want %v", names, want)
	}
}
