package genrepo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// A method promoted from a newly embedded field wins over the derived copy
// a stale output still declares (design D10): nano gen repo never reads a
// method it generated.
func TestStaleOutputNeverHidesAPromotedMethod(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/stale", Scopes: []string{"tenant_id"}})
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
	_, err = Generate(Options{Dir: dir, Scopes: []string{"tenant_id"}})
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
	args := []string{"-dir", dir, "-scopes", "tenant_id"}
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
	if code := Main([]string{"-dir", fixture, "-scopes", "tenant_id", "-explain", "AuditLog"}, &stdout,
		&stderr); code != 0 {
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
	if len(stubs) != 1 || !ok {
		t.Fatalf("stubs for %v, want %s", stubs, derived)
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
