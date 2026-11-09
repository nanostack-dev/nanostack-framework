package genrepo

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest"
)

// fixtureOptions are the options of a package that declares tables of the
// fixture: its tenant columns, their context functions and their short names.
func fixtureOptions(dir string) Options {
	return scopeNameOptions(dir, repotest.ScopeNames)
}

// scopeNameOptions are the options of the packages under testdata/scopenames:
// the fixture's tenant columns, with the short names an application would
// declare in Postgres.ScopeNames.
func scopeNameOptions(dir string, names map[string]string) Options {
	return Options{Dir: dir, Scopes: repotest.Scopes, ContextScopes: repotest.ContextScopes, ScopeNames: names}
}

//nolint:gochecknoglobals // fixture
var shortNames = map[string]string{"tenant_id": "Tenant", "workspace_id": "Workspace"}

// Derived names that name a scope column by its short name generate: the
// short name of a context scope binds no parameter and applies the registered
// function; the Go field names keep deriving.
func TestScopeNamesEmitted(t *testing.T) {
	t.Parallel()
	dir := "testdata/scopenames/emitted"
	res, err := Generate(scopeNameOptions(dir, shortNames))
	if err != nil {
		gentest.Problems(t, err)
		t.Fatal(err)
	}
	var derived []byte
	for _, f := range res.Files {
		if strings.HasSuffix(f.Path, derivedSuffix) {
			derived = f.Data
		}
	}
	if derived == nil {
		t.Fatal("no derived file")
	}
	golden(t, filepath.Join(dir, "want-derived.txt"), derived)
	var explain bytes.Buffer
	WriteExplain(&explain, res.Explain)
	golden(t, filepath.Join(dir, "want-explain.txt"), explain.Bytes())
	src := string(derived)
	for _, want := range []string{
		// No parameter for Workspace; the function is called with ctx and the column.
		"func (r *projectsImpl) FindByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, id string) " +
			"(functional.Option[Project], error) {\n\treturn r.findOne(ctx, r.where.TenantID(tenantID)." +
			"AND(workspacescope.Filter(ctx, r.t.WorkspaceID)).AND(r.where.ID(id)))\n}",
		"\"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope\"",
		// The field form of a context column keeps its parameter and plain equality.
		"func (r *boardsImpl) FindByWorkspaceIDAndID(ctx context.Context, workspaceID string, id string)",
		"r.where.WorkspaceID(workspaceID).AND(r.where.ID(id))",
		// Internal skips the context scope and says so.
		"// UNSCOPED: the name ends in Internal, so it does not filter on workspace_id.",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the derived file has no %q", want)
		}
	}
	if n := strings.Count(src, "workspacescope.Filter("); n < 7 {
		t.Errorf("workspacescope.Filter is called %d times, want at least 7", n)
	}
	if strings.Contains(src, "FindByTenantAndIDInternal(ctx context.Context, tenantID uuid.UUID, id string) "+
		"(functional.Option[Project], error) {\n\treturn r.findOne(ctx, r.where.TenantID(tenantID).AND(workspacescope") {
		t.Error("an Internal read applies the context scope")
	}
}

// A context scope that the name leaves out is refused, with the short-name
// rename and, for a read, Internal; a delete has no escape; a context scope
// does not stand for the hard scope unless it is the table's only one.
func TestScopeNamesRefused(t *testing.T) {
	t.Parallel()
	dir := "testdata/scopenames/refused"
	res, err := Generate(scopeNameOptions(dir, shortNames))
	if err == nil {
		t.Fatal("no error")
	}
	gentest.Problems(t, err)
	golden(t, filepath.Join(dir, "want.txt"), report(nil, err))
	explainGolden(t, filepath.Join(dir, "want-explain.txt"), res)
}

// A short name that is the Go field of another column is refused where the
// table is bound.
func TestScopeNamesClash(t *testing.T) {
	t.Parallel()
	dir := "testdata/scopenames/clash"
	_, err := Generate(scopeNameOptions(dir, map[string]string{"workspace_id": "ID", "tenant_id": "Tenant"}))
	if err == nil {
		t.Fatal("no error")
	}
	gentest.Problems(t, err)
	golden(t, filepath.Join(dir, "want.txt"), report(nil, err))
}

// Without ScopeNames, the context scope is still required, named by its Go
// field: the old names derive, and the refusal suggests the field form.
func TestContextScopeWithoutShortNames(t *testing.T) {
	t.Parallel()
	dir := "testdata/scopenames/fields"
	res, err := Generate(scopeNameOptions(dir, nil))
	if err == nil {
		t.Fatal("no error")
	}
	gentest.Problems(t, err)
	golden(t, filepath.Join(dir, "want.txt"), report(nil, err))
	explainGolden(t, filepath.Join(dir, "want-explain.txt"), res)
}
