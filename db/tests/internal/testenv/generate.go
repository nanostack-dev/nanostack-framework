package testenv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
	"github.com/nanostack-dev/nanostack-framework/db/generator/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/generator/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/generator/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/generator/template"
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	mysqldialect "github.com/nanostack-dev/nanostack-framework/db/mysql"
	pgdialect "github.com/nanostack-dev/nanostack-framework/db/postgres"
	sqlitedialect "github.com/nanostack-dev/nanostack-framework/db/sqlite"

	_ "github.com/go-sql-driver/mysql" // generator drivers
	_ "github.com/mattn/go-sqlite3"
)

// GenTestDataDir is the directory, relative to db/tests, that holds the generated
// models the suites import. It is committed, so the suites compile without a pre-step.
const GenTestDataDir = ".gentestdata"

// OptionDir is the directory, under GenTestDataDir, of the Option-mode models
// (nullable columns as functional.Option[T], models only). The suites use them with
// the pointer-mode table packages, which are the same in both modes.
const OptionDir = "option"

// Schemas and databases generated in Option mode as well (Nanostack).
var (
	optionPgSchemas      = []string{"test_sample", "dvds"}
	optionMySQLDatabases = []string{"test_sample", "dvds"}
	optionSQLiteFiles    = map[string]string{
		"sqlite/test_sample": "test_sample.db",
		"sqlite/sakila":      "sakila.db",
	}
)

// ModelDirs lists the generated directories each source owns under GenTestDataDir.
// PostgreSQL and CockroachDB share jetdb/, MySQL and MariaDB share mysql/, as upstream.
// The Option-mode directories under option/ follow the same split.
func ModelDirs(s Source) []string {
	var dirs []string
	switch {
	case s.IsPostgres() || s == Cockroach:
		for _, schema := range pgSchemas {
			dirs = append(dirs, filepath.Join(pgDBName, schema))
		}
		for _, schema := range optionPgSchemas {
			dirs = append(dirs, filepath.Join(OptionDir, pgDBName, schema))
		}
	case s == MySQL || s == MariaDB:
		for _, name := range mysqlDatabases {
			dirs = append(dirs, filepath.Join("mysql", name))
		}
		for _, name := range optionMySQLDatabases {
			dirs = append(dirs, filepath.Join(OptionDir, "mysql", name))
		}
	default:
		dirs = []string{"sqlite/sakila", "sqlite/chinook", "sqlite/test_sample"}
		for _, dir := range sortedKeys(optionSQLiteFiles) {
			dirs = append(dirs, filepath.Join(OptionDir, dir))
		}
	}
	return dirs
}

// OptionTemplate is the generator template of the Option-mode test models: nullable
// columns as functional.Option[T], and no SQL builder (the pointer-mode one is shared).
// It matches the CLI run with -nullable=option -skip-sql-builder.
func OptionTemplate(dialect jet.Dialect) template.Template {
	return template.DefaultWith(dialect, template.NullableOption).
		UseSchema(func(schema metadata.Schema) template.Schema {
			return template.DefaultSchemaWith(template.NullableOption)(schema).
				UseSQLBuilder(template.DefaultSQLBuilder().ShouldSkip(true))
		})
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SQLiteFiles maps each generated SQLite directory to its database file in the test data.
var sqliteFiles = map[string]string{
	"sqlite/sakila":      "sakila.db",
	"sqlite/chinook":     "chinook.db",
	"sqlite/test_sample": "test_sample.db",
}

// stdoutMu serialises generator runs: the generator prints progress to os.Stdout,
// which the harness silences while it runs.
var stdoutMu sync.Mutex

// GenerateModels runs this repository's generator for s, with the options upstream's
// init program used, into root/.gentestdata. For SQLite, d is nil and sqliteDir holds
// the database files.
func GenerateModels(s Source, d *Database, sqliteDir, root string) error {
	dest := filepath.Join(root, GenTestDataDir)

	return quietStdout(func() error {
		switch {
		case s.IsPostgres() || s == Cockroach:
			return generatePostgres(s, d, dest)
		case s == MySQL || s == MariaDB:
			return generateMySQL(s, d, dest)
		case s == SQLite:
			return generateSQLite(sqliteDir, dest)
		}
		return nil
	})
}

func generatePostgres(s Source, d *Database, dest string) error {
	for _, schema := range pgSchemas {
		if err := postgres.GenerateDSN(d.PostgresDSN(), schema, dest); err != nil {
			return fmt.Errorf("generate %s schema %s: %w", s, schema, err)
		}
	}
	optionDest := filepath.Join(dest, OptionDir)
	for _, schema := range optionPgSchemas {
		err := postgres.GenerateDSN(d.PostgresDSN(), schema, optionDest, OptionTemplate(pgdialect.Dialect))
		if err != nil {
			return fmt.Errorf("generate %s schema %s (option mode): %w", s, schema, err)
		}
	}
	return nil
}

func generateMySQL(s Source, d *Database, dest string) error {
	conn := func(name string) mysql.DBConnection {
		return mysql.DBConnection{Host: d.Host, Port: d.Port, User: user, Password: password, DBName: name}
	}
	for _, name := range mysqlDatabases {
		if err := mysql.Generate(filepath.Join(dest, "mysql"), conn(name)); err != nil {
			return fmt.Errorf("generate %s database %s: %w", s, name, err)
		}
	}
	optionDest := filepath.Join(dest, OptionDir, "mysql")
	for _, name := range optionMySQLDatabases {
		if err := mysql.Generate(optionDest, conn(name), OptionTemplate(mysqldialect.Dialect)); err != nil {
			return fmt.Errorf("generate %s database %s (option mode): %w", s, name, err)
		}
	}
	return nil
}

func generateSQLite(sqliteDir, dest string) error {
	for dir, file := range sqliteFiles {
		if err := sqlite.GenerateDSN(filepath.Join(sqliteDir, file), filepath.Join(dest, dir)); err != nil {
			return fmt.Errorf("generate sqlite %s: %w", file, err)
		}
	}
	optionDest := filepath.Join(dest, OptionDir)
	for dir, file := range optionSQLiteFiles {
		err := sqlite.GenerateDSN(
			filepath.Join(sqliteDir, file), filepath.Join(optionDest, dir), OptionTemplate(sqlitedialect.Dialect),
		)
		if err != nil {
			return fmt.Errorf("generate sqlite %s (option mode): %w", file, err)
		}
	}
	return nil
}

// quietStdout runs f with os.Stdout discarded, and prints what f wrote if it fails.
func quietStdout(f func() error) error {
	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	r, w, err := os.Pipe()
	if err != nil {
		return f()
	}
	saved := os.Stdout
	os.Stdout = w

	captured := make(chan []byte)
	go func() {
		out, _ := io.ReadAll(r)
		captured <- out
	}()

	ferr := f()

	os.Stdout = saved
	_ = w.Close()
	out := <-captured
	_ = r.Close()

	if ferr != nil {
		_, _ = os.Stderr.Write(out)
	}
	return ferr
}
