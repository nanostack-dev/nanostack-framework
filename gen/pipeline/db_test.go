package pipeline

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// The tests of the db step that need no database. dbFixture holds the
// committed models and nanogen.sum of its migrations, so the step hits the
// cache. db_integration_test.go regenerates them on PostgreSQL.

const dbFixture = "testdata/dbapp"

// runDBFixture runs the pipeline on dir with the given database opener, and
// returns the text output as Main prints it.
func runDBFixture(t *testing.T, dir string, opts Options,
	database func(context.Context) (*sql.DB, func(), error),
) (stdout, stderr string, code int) {
	t.Helper()
	opts.Dir = dir
	opts.database = database
	res := Run(opts)
	var out, errOut bytes.Buffer
	writeText(&out, &errOut, res.Events)
	return out.String(), errOut.String(), res.Exit
}

// noDatabase fails the test: a cache hit opens nothing.
func noDatabase(t *testing.T) func(context.Context) (*sql.DB, func(), error) {
	return func(context.Context) (*sql.DB, func(), error) {
		t.Helper()
		t.Fatal("the run opened a database")
		return nil, nil, nil
	}
}

// noDocker is what the run's server reports on a machine without Docker.
func noDocker(context.Context) (*sql.DB, func(), error) {
	return nil, nil, &gen.Error{Msg: "Docker cannot start postgres:18-alpine: no socket",
		Fix: "start Docker, or set NANO_GEN_DATABASE_URL", Err: gen.ErrEnvironment}
}

// The committed models and nanogen.sum match the migrations: -check passes
// and a run writes and deletes nothing, without a database. Before the
// step planned the files nanogen.sum lists, the orphan scan deleted every
// committed model on such a run.
func TestDBCacheHitKeepsTheModels(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	stdout, stderr, code := runDBFixture(t, dir, Options{Check: true}, noDatabase(t))
	wantRun(t, "nanostack gen -check", code, 0, stdout, "", stderr, "")
	stdout, stderr, code = runDBFixture(t, dir, Options{}, noDatabase(t))
	wantRun(t, "nanostack gen", code, 0, stdout, "", stderr, "")
	if _, err := os.Stat(filepath.Join(dir, "internal/db/gen/app/public/table/users.go")); err != nil {
		t.Fatalf("the committed model is gone: %v", err)
	}
}

// A changed migration needs a database. Without one the run exits 3 with
// the reason and the fix, deletes no model, skips repo, which reads the
// table code, and still runs the other steps. The skipped repo step keeps
// its base: it is no orphan.
func TestDBMissWithoutDocker(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	write(t, dir, "migrations/000002_name.up.sql", "ALTER TABLE users ADD COLUMN name text;\n")
	stdout, stderr, code := runDBFixture(t, dir, Options{}, noDocker)
	wantRun(t, "nanostack gen", code, 3, stdout, "", stderr,
		"nanostack gen db: migration 000002_name.up.sql is new since internal/db/gen/app/nanogen.sum, and Docker "+
			"cannot start postgres:18-alpine: no socket\n"+
			"  fix: start Docker, or set NANO_GEN_DATABASE_URL\n"+
			"nanostack gen repo: skipped: needs db\n")
	if _, err := os.Stat(filepath.Join(dir, "internal/db/gen/app/public/table/users.go")); err != nil {
		t.Fatalf("a failed db step deleted a model: %v", err)
	}

	// Without nanogen.sum nothing is planned before the run: the failed
	// step's files must still not count as orphans.
	if err := os.Remove(filepath.Join(dir, "internal/db/gen/app/nanogen.sum")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runDBFixture(t, dir, Options{}, noDocker)
	wantRun(t, "nanostack gen without nanogen.sum", code, 3, stdout, "", stderr,
		"nanostack gen db: internal/db/gen/app/nanogen.sum does not exist, and Docker cannot start "+
			"postgres:18-alpine: no socket\n"+
			"  fix: start Docker, or set NANO_GEN_DATABASE_URL\n"+
			"nanostack gen repo: skipped: needs db\n")
	if _, err := os.Stat(filepath.Join(dir, "internal/db/gen/app/public/table/users.go")); err != nil {
		t.Fatalf("a failed db step without nanogen.sum deleted a model: %v", err)
	}
}

// -fresh ignores nanogen.sum even when it matches: CI proves the models.
func TestDBFreshNeedsADatabase(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	_, stderr, code := runDBFixture(t, dir, Options{Check: true, Fresh: true}, noDocker)
	if code != 3 ||
		!strings.HasPrefix(stderr, "nanostack gen db: -fresh ignores nanogen.sum, and Docker cannot start") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	_, stderr, code = runDBFixture(t, dir, Options{Check: true, Fresh: true, Skip: []string{"db"}}, noDatabase(t))
	wantRun(t, "nanostack gen -check -fresh -skip db", code, 0, "", "", stderr, "")
}

// The Postgres declaration is checked at its line, and a declaration with
// an error skips the steps that read it.
func TestDBDeclarationErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ decl, want string }{
		{`Postgres: &project.Postgres{}`, `nanogen/project.go:7: Postgres.Name is empty
  fix: set Postgres.Name to the directory under Postgres.Out, by convention the database name
`},
		{`Postgres: &project.Postgres{Name: "gen/app"}`, `nanogen/project.go:7: Postgres.Name gen/app is not a directory name
  fix: set Postgres.Name to one directory name, such as the database name; put the parent directories in Postgres.Out
`},
		{`Postgres: &project.Postgres{Name: "app", Migrations: "db/migrations"}`, `nanogen/project.go:7: Postgres.Migrations db/migrations is not a directory
  fix: set Postgres.Migrations to the directory of the SQL migrations
`},
		{`Postgres: &project.Postgres{Name: "app", Out: "../gen"}`, `nanogen/project.go:7: Postgres.Out ../gen is outside the directory nanostack gen runs on
  fix: generate under the directory nanostack gen runs on; a module beside it declares its own nanogen/project.go
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"TenantID", "tenant_id", "tenant_id"}}`, `nanogen/project.go:7: Postgres.Scopes entry "TenantID" is not a lower-case column name
  fix: write each scope as the column name, such as "product_id"
nanogen/project.go:7: Postgres.Scopes lists "tenant_id" twice
  fix: remove the second "tenant_id"
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "workspace_id"}, ` +
			`ContextScopes: []project.ContextScope{{Column: "team_id", Func: "app/scope.Filter"}, ` +
			`{Column: "workspace_id", Func: "Filter"}, {Column: "workspace_id", Func: "app/scope.Filter"}, ` +
			`{Column: "organization_id", Func: "app/scope.1Filter"}}}`,
			`nanogen/project.go:7: Postgres.ContextScopes names "team_id", which is not in Postgres.Scopes
  fix: add "team_id" to Postgres.Scopes, or remove its context scope
nanogen/project.go:7: Postgres.ContextScopes: the function of "workspace_id" is "Filter", not an import path, a dot and a function name
  fix: write the function as "example.com/app/internal/db/workspacescope.Filter"
nanogen/project.go:7: Postgres.ContextScopes names "workspace_id" twice
  fix: register one function per column
nanogen/project.go:7: Postgres.ContextScopes: the function of "organization_id" is "app/scope.1Filter", not an import path, a dot and a function name
  fix: write the function as "example.com/app/internal/db/workspacescope.Filter"
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "workspace_id"}, ` +
			`ScopeNames: map[string]string{"team_id": "Team", "organization_id": "org", "workspace_id": "1Workspace"}}`,
			`nanogen/project.go:7: Postgres.ScopeNames: the name of "organization_id" is "org", not an exported Go identifier
  fix: write an exported Go identifier, such as "Org"
nanogen/project.go:7: Postgres.ScopeNames names "team_id", which is not in Postgres.Scopes
  fix: add "team_id" to Postgres.Scopes, or remove its name
nanogen/project.go:7: Postgres.ScopeNames: the name of "workspace_id" is "1Workspace", not an exported Go identifier
  fix: write an exported Go identifier, such as "Org"
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "workspace_id"}, ` +
			`ScopeNames: map[string]string{"organization_id": "Org", "workspace_id": "Org"}}`,
			`nanogen/project.go:7: Postgres.ScopeNames: "organization_id" and "workspace_id" share the name "Org"
  fix: give each column its own name
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "workspace_id"}, ` +
			`ContextScopes: []project.ContextScope{{Column: "workspace_id", Func: "app/scope.Filter", Stamp: "Stamp"}}}`,
			`nanogen/project.go:7: Postgres.ContextScopes: the stamp function of "workspace_id" is "Stamp", not an import path, a dot and a function name
  fix: write the stamp function as "example.com/app/internal/db/workspacescope.Stamp"
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "workspace_id"}, ` +
			`ContextScopes: []project.ContextScope{{Column: "workspace_id", Func: "app/scope.Filter", Present: "Present"}}}`,
			`nanogen/project.go:7: Postgres.ContextScopes: the present function of "workspace_id" is "Present", not an import path, a dot and a function name
  fix: write the function as "example.com/app/internal/db/workspacescope.Present"
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id"}, ` +
			`DatabaseColumns: []string{"CreatedAt", "created_at", "created_at", "organization_id"}}`,
			`nanogen/project.go:7: Postgres.DatabaseColumns entry "CreatedAt" is not a lower-case column name
  fix: write each column as its name, such as "created_at"
nanogen/project.go:7: Postgres.DatabaseColumns lists "created_at" twice
  fix: remove the second "created_at"
nanogen/project.go:7: Postgres.DatabaseColumns lists the scope column "organization_id"
  fix: remove "organization_id": a derived write sets the scope
`},
		{`Postgres: &project.Postgres{Name: "app", Scopes: []string{"organization_id", "product_id", "workspace_id"}, ` +
			`ContextScopes: []project.ContextScope{{Column: "workspace_id", Func: "example.com/app/scope.Filter"}}, ` +
			`ScopeImplies: map[string]string{"organization_id": "product_id", "product_id": "organization_id", ` +
			`"workspace_id": "organization_id", "team_id": "product_id"}}`,
			`nanogen/project.go:7: Postgres.ScopeImplies has a cycle through "organization_id"
  fix: break the cycle: a column cannot determine itself
nanogen/project.go:7: Postgres.ScopeImplies has a cycle through "product_id"
  fix: break the cycle: a column cannot determine itself
nanogen/project.go:7: Postgres.ScopeImplies names "team_id", which is not in Postgres.Scopes
  fix: add "team_id" to Postgres.Scopes, or remove the entry
nanogen/project.go:7: Postgres.ScopeImplies names "workspace_id", a context column (Postgres.ContextScopes)
  fix: remove the entry: a context scope is named in every derived method, and implies nothing
`},
	} {
		dir := copyFixtureOf(t, dbFixture)
		edit(t, dir, "nanogen/project.go", `Postgres: &project.Postgres{Name: "app"}`, tc.decl)
		_, stderr, code := runDBFixture(t, dir, Options{Check: true}, noDatabase(t))
		want := tc.want + "nanostack gen db: skipped: needs nanogen/project.go\n" +
			"nanostack gen api: skipped: needs nanogen/project.go\n" +
			"nanostack gen validate: skipped: needs nanogen/project.go\n" +
			"nanostack gen repo: skipped: needs nanogen/project.go\n" +
			"nanostack gen config: skipped: needs nanogen/project.go\n"
		if code != 1 || stderr != want {
			t.Errorf("%s: exit %d, stderr:\n%s\nwant:\n%s", tc.decl, code, stderr, want)
		}
	}
}

// A declaration without Postgres makes the db files orphans.
func TestDBOrphansAfterRemovedDeclaration(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	edit(t, dir, "nanogen/project.go", `Postgres: &project.Postgres{Name: "app"},`, "")
	_, stderr, code := runDBFixture(t, dir, Options{Check: true}, noDatabase(t))
	if code != 1 || !strings.HasPrefix(stderr, "orphan internal/db/gen/app/nanogen.sum\n") ||
		!strings.Contains(stderr, "orphan internal/db/gen/app/public/table/users.go\n") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
}
