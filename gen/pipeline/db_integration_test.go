//go:build integration

package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gendb "github.com/nanostack-dev/nanostack-framework/gen/db"
)

// These tests run the db step of the pipeline on PostgreSQL. The runner
// starts its own server, a container of the fixture's image, or uses
// NANO_GEN_DATABASE_URL, as `go tool nano gen` does in a service.
// Run them with: go test -tags integration ./pipeline/

// runReal runs the pipeline with the run's own server.
func runReal(t *testing.T, dir string, opts Options) (stdout, stderr string, code int) {
	t.Helper()
	opts.DatabaseURL = os.Getenv(gendb.EnvDatabaseURL)
	return runDBFixture(t, dir, opts, nil)
}

// `nano gen -check -fresh`, the CI job of a service: the committed models
// and nanogen.sum are what the migrations produce.
func TestDBCheckFresh(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runReal(t, copyFixtureOf(t, dbFixture), Options{Check: true, Fresh: true})
	wantRun(t, "nano gen -check -fresh", code, 0, stdout, "", stderr, "")
}

// A migration that adds a table: -check -fresh lists the new and the
// changed files, a run writes them, and the next run hits the cache. A
// migration that drops the table: the run deletes its files, which no
// longer have a table, as orphans.
func TestDBMigrationsChange(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	write(t, dir, "migrations/000002_notes.up.sql", "CREATE TABLE notes (id text PRIMARY KEY, body text NOT NULL);\n")
	stdout, stderr, code := runReal(t, dir, Options{Check: true, Fresh: true})
	wantRun(t, "nano gen -check -fresh after a migration", code, 1, stdout, "", stderr,
		`missing internal/db/gen/app/public/model/notes.go
missing internal/db/gen/app/public/table/notes.go
stale internal/db/gen/app/public/table/table_use_schema.go
stale internal/db/gen/app/nanogen.sum
  fix: run nano gen without -check, then commit the result
`)

	stdout, stderr, code = runReal(t, dir, Options{})
	wantRun(t, "nano gen after a migration", code, 0, stdout, `wrote internal/db/gen/app/public/model/notes.go
wrote internal/db/gen/app/public/table/notes.go
wrote internal/db/gen/app/public/table/table_use_schema.go
wrote internal/db/gen/app/nanogen.sum
`, stderr, "")
	keys, err := os.ReadFile(filepath.Join(dir, "internal/db/gen/app/public/table/notes.go"))
	if err != nil || !strings.Contains(string(keys), `"notes_pkey": {"id"},`) {
		t.Fatalf("notes.go has no UniqueKeys with notes_pkey (err %v):\n%s", err, keys)
	}
	stdout, stderr, code = runDBFixture(t, dir, Options{Check: true}, noDatabase(t))
	wantRun(t, "nano gen -check after the run", code, 0, stdout, "", stderr, "")

	write(t, dir, "migrations/000003_drop_notes.up.sql", "DROP TABLE notes;\n")
	stdout, stderr, code = runReal(t, dir, Options{})
	wantRun(t, "nano gen after DROP TABLE", code, 0, stdout, `wrote internal/db/gen/app/public/table/table_use_schema.go
wrote internal/db/gen/app/nanogen.sum
deleted internal/db/gen/app/public/model/notes.go
deleted internal/db/gen/app/public/table/notes.go
`, stderr, "")
}

// A migration that drops a key: -check -fresh fails on the table file whose
// UniqueKeys lost it, so the pull request that drops a key a derived method
// relies on cannot pass CI with stale code.
func TestDBDroppedKeyIsStale(t *testing.T) {
	t.Parallel()
	dir := copyFixtureOf(t, dbFixture)
	write(t, dir, "migrations/000002_drop_key.up.sql", "ALTER TABLE users DROP CONSTRAINT users_tenant_email_key;\n")
	stdout, stderr, code := runReal(t, dir, Options{Check: true, Fresh: true})
	wantRun(t, "nano gen -check -fresh after a dropped key", code, 1, stdout, "", stderr,
		`stale internal/db/gen/app/public/table/users.go
stale internal/db/gen/app/nanogen.sum
  fix: run nano gen without -check, then commit the result
`)
}
