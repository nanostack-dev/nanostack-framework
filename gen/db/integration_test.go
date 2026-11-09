//go:build integration

package gendb

import (
	"context"
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	jetpostgres "github.com/nanostack-dev/nanostack-framework/db/generator/postgres"
	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// These tests run the step on PostgreSQL: a container of the image the
// keys fixture declares (TimescaleDB, as anchor), or NANO_GEN_DATABASE_URL.
// Run them with: go test -tags integration ./db/
//
// testdata/keys holds a migration with one unique index per shape that the
// key rule classifies, and golden/keys the committed output. Rewrite it with
// UPDATE_GOLDEN=1 after a deliberate change, and review the diff.

const (
	keysDir   = "testdata/keys"
	keysImage = "timescale/timescaledb:2.23.0-pg18"
)

// keys is the declaration of the fixture, as a service would write it.
var keys = project.Postgres{Name: "keys", Out: "golden", Image: keysImage} //nolint:gochecknoglobals // fixture

// server is the one server of the test binary.
var server = sync.OnceValue(func() *Server { //nolint:gochecknoglobals // one server per test binary
	return NewServer(os.Getenv(EnvDatabaseURL), keysImage)
})

func TestMain(m *testing.M) {
	code := m.Run()
	server().Close()
	os.Exit(code)
}

// open is the Opener of the tests: a scratch database on the shared server.
func open(ctx context.Context) (*sql.DB, func(), error) { return server().Open(ctx) }

// The committed golden output is what the migrations produce, byte for
// byte, nanogen.sum included, and no other nano gen db file is there.
func TestGolden(t *testing.T) {
	opts := Options{Dir: keysDir, Postgres: keys, Fresh: true, Database: open, Check: os.Getenv("UPDATE_GOLDEN") == ""}
	written, err := run(opts)
	if err != nil {
		t.Fatalf("%v\nrun UPDATE_GOLDEN=1 go test -tags integration ./db/ and review the diff", err)
	}
	if len(written) > 0 {
		t.Logf("rewrote %d golden files", len(written))
	}
}

// Every unique index of every table of the fixture is either a key of its
// table's UniqueKeys, with the same key columns, or named on a "Not a key"
// line; and every key is a unique index of that table. The rule itself is
// TestNotKey's (db/generator/postgres); this test checks that the catalogue
// query and the emitted code agree with the live catalogue.
func TestUniqueKeysMatchCatalog(t *testing.T) {
	db, drop := migrated(t)
	defer drop()
	rows, err := db.QueryContext(t.Context(), `
SELECT c.relname, i.relname,
       to_json(ARRAY(SELECT COALESCE(a.attname, '')
                     FROM unnest((ix.indkey::int2[])[0:ix.indnkeyatts - 1]) WITH ORDINALITY AS k(attnum, ord)
                     LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
                     ORDER BY k.ord))::text
FROM pg_index ix
JOIN pg_class c ON c.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND ix.indisunique`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	catalog := map[string]map[string][]string{} // table -> index -> key columns
	for rows.Next() {
		var table, index, cols string
		if err = rows.Scan(&table, &index, &cols); err != nil {
			t.Fatal(err)
		}
		var columns []string
		if err = json.Unmarshal([]byte(cols), &columns); err != nil {
			t.Fatal(err)
		}
		if catalog[table] == nil {
			catalog[table] = map[string][]string{}
		}
		catalog[table][index] = columns
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(keysDir, "golden/keys/public/table/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range files {
		table, emitted, notKeys, ok := parseUniqueKeys(t, file)
		if !ok {
			continue // table_use_schema.go
		}
		seen++
		for index, columns := range emitted {
			if want, found := catalog[table][index]; !found || !slices.Equal(columns, want) {
				t.Errorf("%s: key %s %v is not a unique index of %s with those key columns (catalogue: %v)",
					file, index, columns, table, want)
			}
		}
		for index := range catalog[table] {
			if _, isKey := emitted[index]; !isKey && !slices.Contains(notKeys, index) {
				t.Errorf("%s: unique index %s is neither a key nor a \"Not a key\" line", file, index)
			}
		}
	}
	if seen != len(catalog)+1 { // events has no unique index, so no catalogue row
		t.Fatalf("checked %d table files, the catalogue has unique indexes on %d tables", seen, len(catalog))
	}
}

// parseUniqueKeys reads the UniqueKeys method of a table file with
// go/parser, as nano gen repo does: the table name, the keys and the names
// on the "Not a key" lines.
func parseUniqueKeys(t *testing.T, file string) (table string, keys map[string][]string, notKeys []string, ok bool) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fn.Name.Name != "UniqueKeys" || fn.Recv == nil {
			continue
		}
		for _, c := range fn.Doc.List {
			text := strings.TrimPrefix(c.Text, "// ")
			if rest, found := strings.CutPrefix(text, "Not a key: "); found {
				name, _, _ := strings.Cut(rest, ":")
				notKeys = append(notKeys, name)
			}
			if rest, found := strings.CutPrefix(text, "UniqueKeys lists the keys of "); found {
				table, _, _ = strings.Cut(rest, " ")
			}
		}
		ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
		if !isReturn || len(ret.Results) != 1 {
			t.Fatalf("%s: UniqueKeys does not return one map literal", file)
		}
		lit, isLit := ret.Results[0].(*ast.CompositeLit)
		if !isLit {
			t.Fatalf("%s: UniqueKeys does not return a map literal", file)
		}
		keys = map[string][]string{}
		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			cols, isCols := kv.Value.(*ast.CompositeLit)
			if !isKV || !isCols {
				t.Fatalf("%s: a UniqueKeys entry is not name: {columns}", file)
			}
			var columns []string
			for _, c := range cols.Elts {
				columns = append(columns, unquote(t, c))
			}
			keys[unquote(t, kv.Key)] = columns
		}
		return table, keys, notKeys, true
	}
	return "", nil, nil, false
}

// unquote returns the value of a string literal.
func unquote(t *testing.T, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		t.Fatalf("%T is not a string literal", e)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// An index that CREATE INDEX CONCURRENTLY left invalid is not a key. The
// test marks a key invalid in the catalogue, as a failed concurrent build
// leaves it, because a migration cannot build one: golang-migrate runs each
// file in a transaction block.
func TestInvalidIndexIsNotAKey(t *testing.T) {
	db, drop := migrated(t)
	defer drop()
	if _, err := db.ExecContext(t.Context(),
		`UPDATE pg_index SET indisvalid = false WHERE indexrelid = 'idx_users_handle'::regclass`); err != nil {
		t.Skipf("the server's user may not update pg_index: %v", err)
	}
	schema, err := jetpostgres.ReadSchema(db, "public")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range schema.TablesMetaData {
		for _, ix := range table.UniqueIndexes {
			if ix.Name == "idx_users_handle" {
				if ix.NotKey != "not valid (CREATE INDEX CONCURRENTLY did not finish)" {
					t.Fatalf("NotKey = %q", ix.NotKey)
				}
				return
			}
		}
	}
	t.Fatal("idx_users_handle not found")
}

// A run generated from a database leaves a nanogen.sum that the next run
// hits: it opens no database and writes nothing.
func TestGeneratedSumHits(t *testing.T) {
	dir := copyDir(t, keysDir)
	p := keys
	p.Out = "internal/db/gen"
	if _, err := run(Options{Dir: dir, Postgres: p, Database: open}); err != nil {
		t.Fatal(err)
	}
	written, err := run(Options{Dir: dir, Postgres: p, Database: noDatabase(t)})
	if err != nil || len(written) > 0 {
		t.Fatalf("second run: wrote %v, err %v", written, err)
	}
}

// A migration that fails is a source error at the migrations directory.
func TestFailingMigration(t *testing.T) {
	dir := copyDir(t, keysDir)
	write(t, dir, "migrations/000002_broken.up.sql", "ALTER TABLE no_such_table ADD COLUMN x int;\n")
	_, err := Generate(Options{Dir: dir, Postgres: keys, Fresh: true, Database: open})
	problems := gen.Errors(err)
	if gen.ExitCode(err) != 1 || len(problems) != 1 || problems[0].File != "migrations" ||
		!strings.Contains(problems[0].Msg, "the migrations fail on an empty database") ||
		!strings.Contains(problems[0].Msg, "no_such_table") {
		t.Fatalf("exit %d, error:\n%v", gen.ExitCode(err), err)
	}
}

// Migrations that create nothing in the declared schema are an error that
// names the schema.
func TestEmptySchema(t *testing.T) {
	dir := copyDir(t, keysDir)
	p := keys
	p.Schema = "billing"
	_, err := Generate(Options{Dir: dir, Postgres: p, Fresh: true, Database: open})
	if err == nil || !strings.Contains(err.Error(), "the migrations leave schema billing empty") {
		t.Fatalf("error:\n%v", err)
	}
}

// With NANO_GEN_DATABASE_URL, each Open creates a scratch database on that
// server and its drop removes it.
func TestDatabaseURL(t *testing.T) {
	if _, _, err := server().Open(t.Context()); err != nil { // start the shared server
		t.Fatal(err)
	}
	s := NewServer(server().adminDSN, "")
	defer s.Close()
	db, drop, err := s.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = db.QueryRowContext(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "nanogen_") {
		t.Fatalf("current_database() = %q, want a nanogen_ scratch database", name)
	}
	drop()
	admin, err := sql.Open("postgres", server().adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	var exists bool
	if err = admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)",
		name).Scan(&exists); err != nil || exists {
		t.Fatalf("scratch database %s still exists after drop (err %v)", name, err)
	}
}

// migrated returns a scratch database with the fixture's migrations.
func migrated(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, drop, err := open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg := nanomigrate.DefaultConfig()
	cfg.BasePath, _ = filepath.Abs(filepath.Join(keysDir, "migrations"))
	cfg.Validate = false
	if err = nanomigrate.NewMigrator(cfg, db, slog.New(slog.DiscardHandler)).Migrate(t.Context()); err != nil {
		drop()
		t.Fatal(err)
	}
	return db, drop
}

// copyDir copies the migrations of a fixture to a temporary directory.
func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(src, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, readErr := os.ReadFile(filepath.Join(src, "migrations", e.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		write(t, dst, "migrations/"+e.Name(), string(data))
	}
	return dst
}
