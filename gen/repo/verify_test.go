package genrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture"
)

// mutateIn replaces from by to inside the method of the receiver impl called
// name, and reports whether it found from there.
func mutateIn(src, impl, name, from, to string) (string, bool) {
	head := "func (r *" + impl + ") " + name + "("
	start := strings.Index(src, head)
	if start < 0 {
		return src, false
	}
	end := len(src)
	if next := strings.Index(src[start+1:], "\nfunc "); next >= 0 {
		end = start + 1 + next
	}
	body := src[start:end]
	if !strings.Contains(body, from) {
		return src, false
	}
	return src[:start] + strings.Replace(body, from, to, 1) + src[end:], true
}

// diskRead reads the derived files of the torture fixture from disk.
func diskRead(name string) ([]byte, error) { return os.ReadFile(filepath.Join(tortureFixture, name)) }

// No derived method of the torture fixture omits a scope column. The check
// reads the committed derived files, takes each table's scope columns from
// its generated table package, and trusts no Postgres.ScopeImplies entry that
// the table does not prove with a foreign key.
func TestTortureEveryDerivedMethodCoversEveryScope(t *testing.T) {
	t.Parallel()
	opts := tortureOptions(tortureFixture)
	res, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyFiles(res.resolutions, opts, diskRead)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Violations) > 0 {
		t.Fatalf("derived methods that leave a scope column uncovered:\n%s", strings.Join(got.Violations, "\n"))
	}
	want := 0
	for _, r := range res.resolutions {
		want += len(r.derived)
	}
	if got.Methods != want || got.Methods < 280 {
		t.Errorf("%d derived methods read, want %d (at least 280)", got.Methods, want)
	}
	// The public entry point reads the same files.
	pub, err := VerifyScopes(opts)
	if err != nil || len(pub.Violations) > 0 || pub.Methods != got.Methods {
		t.Errorf("VerifyScopes: %+v, %v", pub, err)
	}
}

// A dropped scope is caught in every shape of method: the predicate of a
// read and of a delete, the WHERE of an update, the stamped columns of a
// create and an upsert, the conflict target and the DO UPDATE guard of an
// upsert, and the Present guard of a delete or a write. An implication that
// the table does not prove covers nothing.
func TestTortureVerifyCatchesADroppedScope(t *testing.T) {
	t.Parallel()
	opts := tortureOptions(tortureFixture)
	res, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	const (
		wides  = "widesImpl"
		shelf  = "wShelvesImpl"
		upsert = "UpsertByOrgAndProductAndWorkspaceAndSlug"
		update = "UpdateByOrgAndProductAndWorkspaceAndID"
		create = "CreateInOrgAndProductAndWorkspace"
		guard  = "\tif !workspacescope.Present(ctx) {\n\t\treturn *new(model.Wide), nanorepo.ErrNoContextScope\n\t}\n"
	)
	mutations := []struct{ file, impl, method, from, to, what string }{
		{"repositories_repo.gen.go", "ordersImpl", "FindByOrgAndProductAndWorkspaceAndName",
			"r.where.ProductID(productID)).AND(", "", "a read drops product_id"},
		{"repositories_repo.gen.go", "ordersImpl", "DeleteByOrgAndProductAndWorkspaceAndName",
			"r.where.OrgID(orgID).AND(", "(", "a delete drops org_id"},
		{"repositories_repo.gen.go", "notesImpl", "FindByWorkspaceAndTitle",
			"workspacescope.Filter(ctx, r.t.WorkspaceID).AND(", "(", "a read drops the context scope"},
		{"repositories_repo.gen.go", "shelvesImpl", "FindByOrgAndProductAndName",
			"r.where.OrgID(orgID).AND(", "(", "a read of a table whose implication is not proven drops org_id"},
		{"writes_repo.gen.go", wides, update, ".AND(r.where.ProductID(productID))", "", "an update drops product_id"},
		{
			"writes_repo.gen.go",
			wides,
			update,
			".AND(workspacescope.Filter(ctx, r.t.WorkspaceID))",
			"",
			"an update drops the context scope",
		},
		{
			"writes_repo.gen.go",
			shelf,
			update,
			".AND(r.where.ProductID(productID))",
			"",
			"an update without a result drops product_id",
		},
		{
			"writes_repo.gen.go",
			wides,
			create,
			"row.ProductID = productID",
			"_ = productID",
			"a create does not stamp product_id",
		},
		{"writes_repo.gen.go", wides, create, "row.OrgID = orgID", "_ = orgID", "a create does not stamp org_id"},
		{"writes_repo.gen.go", wides, create, "row.WorkspaceID, err = workspacescope.Stamp(ctx)",
			"_, err = workspacescope.Stamp(ctx)", "a create does not stamp the workspace"},
		{"writes_repo.gen.go", wides, upsert, "row.OrgID = orgID", "_ = orgID", "an upsert does not stamp org_id"},
		{
			"writes_repo.gen.go",
			wides,
			upsert,
			"ON_CONFLICT(r.t.OrgID, ",
			"ON_CONFLICT(",
			"an upsert conflicts without org_id",
		},
		{
			"writes_repo.gen.go",
			wides,
			upsert,
			").WHERE(r.t.WorkspaceID.EQ(r.t.EXCLUDED.WorkspaceID)))",
			"))",
			"an upsert drops the workspace guard",
		},
		{
			"writes_repo.gen.go",
			shelf,
			upsert,
			"r.t.OrgID.EQ(r.t.EXCLUDED.OrgID).AND(",
			"(",
			"an upsert guard drops org_id",
		},
		{"writes_repo.gen.go", wides, create, guard, "", "a create does not check Present"},
		{"writes_repo.gen.go", wides, update, guard, "", "an update does not check Present"},
		{
			"repositories_repo.gen.go",
			"notesImpl",
			"DeleteByWorkspaceAndTitle",
			"\tif !workspacescope.Present(ctx) {\n\t\treturn 0, nanorepo.ErrNoContextScope\n\t}\n",
			"",
			"a delete does not check Present",
		},
	}
	for _, m := range mutations {
		mutated, found := false, false
		read := func(name string) ([]byte, error) {
			data, rerr := diskRead(name)
			if rerr != nil || name != m.file {
				return data, rerr
			}
			out, ok := mutateIn(string(data), m.impl, m.method, m.from, m.to)
			mutated, found = true, found || ok
			return []byte(out), nil
		}
		got, verr := verifyFiles(res.resolutions, opts, read)
		if verr != nil {
			t.Fatal(verr)
		}
		if !mutated || !found {
			t.Errorf("mutation %q: the emitted code has no %q in %s", m.what, m.from, m.method)
			continue
		}
		if len(got.Violations) == 0 {
			t.Errorf("mutation %q is not caught", m.what)
		}
	}
	// An implication that the table proves covers the column: bin proves
	// site -> product -> org, so a read by Site alone needs no org predicate.
	read := func(name string) ([]byte, error) {
		data, rerr := diskRead(name)
		if rerr != nil || name != "repositories_repo.gen.go" {
			return data, rerr
		}
		out, _ := mutateIn(
			string(data),
			"binsImpl",
			"FindByOrgAndProductAndSiteAndName",
			"r.where.OrgID(orgID).AND(",
			"(",
		)
		return []byte(out), nil
	}
	if got, verr := verifyFiles(res.resolutions, opts, read); verr != nil || len(got.Violations) > 0 {
		t.Errorf("a column that a proven implication covers is flagged: %v %v", got, verr)
	}
}

// torturePairs are the flags that declare the torture project to the single run.
func torturePairs() []string {
	var ctxScopes, names, stamps, presents []string
	for col, fn := range torture.ContextScopes {
		ctxScopes = append(ctxScopes, col+"="+fn)
	}
	for col, n := range torture.ScopeNames {
		names = append(names, col+"="+n)
	}
	for col, fn := range torture.ContextStamps {
		stamps = append(stamps, col+"="+fn)
	}
	for col, fn := range torture.ContextPresents {
		presents = append(presents, col+"="+fn)
	}
	var implies []string
	for col, to := range torture.ScopeImplies {
		implies = append(implies, col+"="+to)
	}
	return []string{
		"-dir",
		tortureFixture,
		"-scopes",
		strings.Join(torture.Scopes, ","),
		"-context-scopes",
		strings.Join(ctxScopes, ","),
		"-scope-names",
		strings.Join(names, ","),
		"-context-stamps",
		strings.Join(stamps, ","),
		"-context-present",
		strings.Join(presents, ","),
		"-context-fail-open",
		"region_id",
		"-scope-implies",
		strings.Join(implies, ","),
		"-database-columns",
		strings.Join(torture.DatabaseColumns, ","),
	}
}

// -verify-scopes runs the coverage check from the command line, and -inventory
// lists the methods a review looks at.
func TestMainVerifyScopesAndInventory(t *testing.T) {
	t.Parallel()
	var out, errOut strings.Builder
	if code := Main(append(torturePairs(), "-verify-scopes"), &out, &errOut); code != 0 {
		t.Fatalf("-verify-scopes: exit %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "every one covers every scope column") {
		t.Errorf("-verify-scopes printed %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Main(append(torturePairs(), "-inventory"), &out, &errOut); code != 0 {
		t.Fatalf("-inventory: exit %d\n%s", code, errOut.String())
	}
	for _, want := range []string{"Orders.FindByOrgAndProductAndNameInternal", "UNSCOPED", "CROSS-WORKSPACE"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("-inventory lacks %q:\n%s", want, out.String())
		}
	}
}

// The -scope-implies flag is guarded as the project file is: both sides in
// -scopes, no context column, no cycle.
func TestMainScopeImpliesFlagIsChecked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ implies, want string }{
		{"site_id=team_id", `names "team_id", which is not in Postgres.Scopes`},
		{"workspace_id=org_id", `names "workspace_id", a context column`},
		{"org_id=product_id,product_id=org_id", `has a cycle through "org_id"`},
	} {
		var out, errOut strings.Builder
		args := append(torturePairs(), "-scope-implies", tc.implies) // the last flag wins
		if code := Main(args, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf(
				"-scope-implies %s: exit %d, stderr %q, want exit 2 with %q",
				tc.implies,
				code,
				errOut.String(),
				tc.want,
			)
		}
	}
}

// The check is structural: a predicate counts only as a conjunct of the
// condition, a Present guard only when it calls the registered function, and
// a read of a table that has only a context scope needs the guard too.
func TestTortureVerifyIsStructural(t *testing.T) {
	t.Parallel()
	opts := tortureOptions(tortureFixture)
	res, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	const repos = "repositories_repo.gen.go"
	const regionImport = "\n\t\"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/regionscope\""
	for _, m := range []struct{ file, impl, method, from, to, imp, what string }{
		{repos, "ordersImpl", "FindByOrgAndProductAndWorkspaceAndName",
			").AND(workspacescope.Filter(", ").OR(workspacescope.Filter(", "", "AND swapped for OR in a read"},
		{repos, "ordersImpl", "DeleteByOrgAndProductAndWorkspaceAndName",
			"r.where.OrgID(orgID).AND(", "r.where.OrgID(orgID).OR(", "", "AND swapped for OR in a delete"},
		{repos, "ordersImpl", "FindByOrgAndProductAndWorkspaceAndName",
			"r.where.OrgID(orgID).AND(", "r.where.OrgID(orgID).NOT().AND(", "", "a predicate negated in a read"},
		{"writes_repo.gen.go", "widesImpl", "UpdateByOrgAndProductAndWorkspaceAndID",
			".AND(r.where.ID(row.ID))", ".OR(r.where.ID(row.ID))", "", "AND swapped for OR in an update"},
		{repos, "notesImpl", "FindByWorkspaceAndTitle", "workspacescope.Present(ctx)", "regionscope.Present(ctx)",
			regionImport, "a read of a context-only table calls another Present"},
		{repos, "notesImpl", "FindByWorkspaceAndTitle",
			"\tif !workspacescope.Present(ctx) {\n\t\treturn nil, nanorepo.ErrNoContextScope\n\t}\n", "", "",
			"a read of a context-only table has no Present guard"},
		{"writes_repo.gen.go", "widesImpl", "UpdateByOrgAndProductAndWorkspaceAndID", "workspacescope.Present(ctx)",
			"regionscope.Present(ctx)", regionImport, "an update calls another Present"},
	} {
		found := false
		read := func(name string) ([]byte, error) {
			data, rerr := diskRead(name)
			if rerr != nil || name != m.file {
				return data, rerr
			}
			out, ok := mutateIn(string(data), m.impl, m.method, m.from, m.to)
			found = ok
			if m.imp != "" {
				out = strings.Replace(out, "import (", "import ("+m.imp, 1)
			}
			return []byte(out), nil
		}
		got, verr := verifyFiles(res.resolutions, opts, read)
		if verr != nil {
			t.Fatal(verr)
		}
		if !found {
			t.Errorf("mutation %q: no %q in %s", m.what, m.from, m.method)
		} else if len(got.Violations) == 0 {
			t.Errorf("mutation %q is not caught", m.what)
		}
	}
}

// -inventory also lists the hand-written methods whose names derive, which
// VerifyScopes cannot read, and the fail-open choices a project reviewed.
func TestInventoryListsOverridesAndFailOpen(t *testing.T) {
	t.Parallel()
	var out, errOut strings.Builder
	opts := fixtureOptions(fixture)
	res, err := Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if code := mainInventory(opts, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"HAND-WRITTEN (derivable)", "FAIL-OPEN (reviewed)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("-inventory lacks %q:\n%s", want, out.String())
		}
	}
	overrides := 0
	for _, r := range res.resolutions {
		overrides += len(r.overrides)
	}
	if got := strings.Count(out.String(), "HAND-WRITTEN (derivable)"); got != overrides || overrides == 0 {
		t.Errorf("%d hand-written lines for %d overrides", got, overrides)
	}
}
