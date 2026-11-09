package gendb

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	"github.com/nanostack-dev/nanostack-framework/gen"
)

// These tests need no database: they cover nanogen.sum, the reasons a run
// needs a database, and the environment errors. integration_test.go runs
// the step on PostgreSQL.

const (
	testMigration = "000001_init.up.sql"
	testModel     = "public/model/users.go"
	testTable     = "public/table/users.go"
)

// cachedModule writes a module whose nanogen.sum matches its migration and
// its two outputs, and returns its directory and declaration.
func cachedModule(t *testing.T) (string, project.Postgres) {
	t.Helper()
	dir := t.TempDir()
	p := project.Postgres{Name: "app"}
	write(t, dir, "migrations/"+testMigration, "CREATE TABLE users (id text PRIMARY KEY);\n")
	write(t, dir, "internal/db/gen/app/"+testModel, header+"package model\n")
	write(t, dir, "internal/db/gen/app/"+testTable, header+"package table\n")
	writeSum(t, dir, p)
	return dir, p
}

// writeSum records the current inputs and outputs of the module in dir.
func writeSum(t *testing.T, dir string, p project.Postgres) {
	t.Helper()
	l, err := newLayout(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := readMigrations(l)
	if err != nil {
		t.Fatal(err)
	}
	s := sumFile{inputs: inputLines(l.p, migrations)}
	for _, rel := range []string{testModel, testTable} {
		data, readErr := os.ReadFile(filepath.Join(l.out, rel))
		if readErr != nil {
			continue // a test removed it after the sum
		}
		s.outputs = append(s.outputs, line{kind: "output", path: rel, hash: digest(data)})
	}
	write(t, dir, "internal/db/gen/app/"+SumFile, string(s.bytes()))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// noDatabase is an Opener that fails the test: a cache hit opens nothing.
func noDatabase(t *testing.T) Opener {
	return func(context.Context) (*sql.DB, func(), error) {
		t.Helper()
		t.Fatal("the run opened a database")
		return nil, nil, nil
	}
}

// unreachableDatabase is an Opener that reports what a Server reports
// without Docker.
func unreachableDatabase(context.Context) (*sql.DB, func(), error) {
	return nil, nil, &gen.Error{Msg: "Docker cannot start postgres:18-alpine: no socket",
		Fix: "start Docker, or set " + EnvDatabaseURL, Err: gen.ErrEnvironment}
}

// A run whose inputs and outputs match nanogen.sum returns the committed
// files, nanogen.sum included, and opens no database.
func TestCacheHit(t *testing.T) {
	t.Parallel()
	dir, p := cachedModule(t)
	files, err := Generate(Options{Dir: dir, Postgres: p, Database: noDatabase(t)})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		rel, _ := filepath.Rel(filepath.Join(dir, "internal/db/gen/app"), f.Path)
		got = append(got, filepath.ToSlash(rel))
		have, _ := os.ReadFile(f.Path)
		if !bytes.Equal(have, f.Data) {
			t.Errorf("%s: the returned data differs from the disk", rel)
		}
	}
	if want := []string{testModel, testTable, SumFile}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	written, err := run(Options{Dir: dir, Postgres: p, Database: noDatabase(t), Check: true})
	if err != nil || len(written) > 0 {
		t.Fatalf("Run -check on a cache hit: wrote %v, err %v", written, err)
	}
}

// Every way the cache can miss names its reason in the error a run without
// a database returns, which exits 3 with the fix.
func TestCacheMissNeedsADatabase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string, p *project.Postgres)
		fresh  bool
		want   string
	}{
		{"no sum", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, "internal/db/gen/app", SumFile)); err != nil {
				t.Fatal(err)
			}
		}, false, "internal/db/gen/app/nanogen.sum does not exist"},
		{"changed migration", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "migrations/"+testMigration, "CREATE TABLE users (id bigint PRIMARY KEY);\n")
		}, false, "migration 000001_init.up.sql changed since internal/db/gen/app/nanogen.sum"},
		{"new migration", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "migrations/000002_more.up.sql", "ALTER TABLE users ADD COLUMN name text;\n")
		}, false, "migration 000002_more.up.sql is new since internal/db/gen/app/nanogen.sum"},
		{"removed migration", func(t *testing.T, dir string, p *project.Postgres) {
			t.Helper()
			write(t, dir, "migrations/000002_more.up.sql", "ALTER TABLE users ADD COLUMN name text;\n")
			writeSum(t, dir, *p)
			if err := os.Remove(filepath.Join(dir, "migrations/000002_more.up.sql")); err != nil {
				t.Fatal(err)
			}
		}, false, "migration 000002_more.up.sql was removed since internal/db/gen/app/nanogen.sum"},
		{"other image", func(_ *testing.T, _ string, p *project.Postgres) {
			p.Image = "timescale/timescaledb:2.23.0-pg18"
		}, false, "image changed from postgres:18-alpine to timescale/timescaledb:2.23.0-pg18 since " +
			"internal/db/gen/app/nanogen.sum"},
		{"edited output", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "internal/db/gen/app/"+testTable, header+"package table // edited\n")
		}, false, "internal/db/gen/app/public/table/users.go differs from internal/db/gen/app/nanogen.sum"},
		{"missing output", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, "internal/db/gen/app", testModel)); err != nil {
				t.Fatal(err)
			}
		}, false, "internal/db/gen/app/public/model/users.go is missing"},
		{"unlisted output", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "internal/db/gen/app/public/model/old.go", header+"package model\n")
		}, false, "internal/db/gen/app/public/model/old.go is not in internal/db/gen/app/nanogen.sum"},
		{"unparsable sum", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "internal/db/gen/app/"+SumFile, "# Code generated by nano gen db. DO NOT EDIT.\nbogus\n")
		}, false, `internal/db/gen/app/nanogen.sum does not parse (line 2: unknown line "bogus")`},
		{"output outside the directory of the sum", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			write(t, dir, "internal/db/gen/app/"+SumFile, "output "+strings.Repeat("0", 64)+" ../../../../go.mod\n")
		}, false, `internal/db/gen/app/nanogen.sum does not parse (line 1: an output is "output <sha256> <path>", ` +
			`with a path under the directory of nanogen.sum)`},
		{"changed migration behind a symbolic link", func(t *testing.T, dir string, p *project.Postgres) {
			t.Helper()
			// golang-migrate applies a linked migration file, so its
			// content is an input like any other.
			write(t, dir, "shared/000002_more.up.sql", "ALTER TABLE users ADD COLUMN name text;\n")
			if err := os.Symlink(filepath.Join(dir, "shared/000002_more.up.sql"),
				filepath.Join(dir, "migrations/000002_more.up.sql")); err != nil {
				t.Fatal(err)
			}
			writeSum(t, dir, *p)
			write(t, dir, "shared/000002_more.up.sql", "ALTER TABLE users ADD COLUMN name bigint;\n")
		}, false, "migration 000002_more.up.sql changed since internal/db/gen/app/nanogen.sum"},
		{"fresh", func(*testing.T, string, *project.Postgres) {}, true, "-fresh ignores nanogen.sum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, p := cachedModule(t)
			tc.change(t, dir, &p)
			_, err := Generate(Options{Dir: dir, Postgres: p, Fresh: tc.fresh, Database: unreachableDatabase})
			problems := gen.Errors(err)
			want := tc.want + ", and Docker cannot start postgres:18-alpine: no socket"
			if len(problems) != 1 || problems[0].Msg != want {
				t.Fatalf("error %v\nwant msg %q", err, want)
			}
			if problems[0].Fix != "start Docker, or set NANO_GEN_DATABASE_URL" || gen.ExitCode(err) != 3 {
				t.Fatalf("fix %q, exit %d; want the Docker fix and exit 3", problems[0].Fix, gen.ExitCode(err))
			}
		})
	}
}

// A Server whose container does not start, as on a machine without
// Docker, gives the environment error with the fix, and the run exits 3.
// testcontainers panics on some hosts it cannot reach: the same error.
func TestNoDockerExitsThree(t *testing.T) {
	t.Parallel()
	for _, start := range []func(context.Context, string) (testcontainers.Container, error){
		func(context.Context, string) (testcontainers.Container, error) {
			return nil, errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
		},
		func(context.Context, string) (testcontainers.Container, error) {
			panic("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
		},
	} {
		dir, p := cachedModule(t)
		server := NewServer("", "")
		server.run = start
		_, err := Generate(Options{Dir: dir, Postgres: p, Fresh: true, Database: server.Open})
		problems := gen.Errors(err)
		want := "-fresh ignores nanogen.sum, and Docker cannot start postgres:18-alpine: " +
			"Cannot connect to the Docker daemon at unix:///var/run/docker.sock"
		if gen.ExitCode(err) != 3 || len(problems) != 1 || problems[0].Msg != want ||
			problems[0].Fix != "start Docker, or set NANO_GEN_DATABASE_URL" {
			t.Fatalf("exit %d, error:\n%v", gen.ExitCode(err), err)
		}
	}
}

// A NANO_GEN_DATABASE_URL that is not a postgres URL, or that names a
// server that does not answer, is an environment error with its own fix,
// and the message never shows the password.
func TestDatabaseURLErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ url, msg, fix string }{
		{"mysql://root@localhost/x", "NANO_GEN_DATABASE_URL is not a postgres:// URL",
			"set NANO_GEN_DATABASE_URL to a URL such as postgres://user:password@localhost:5432/postgres, " +
				"or unset it to start a container"},
		{"postgres://nanogen:s3cret@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=2",
			"the server that NANO_GEN_DATABASE_URL names (postgres://nanogen:xxxxx@127.0.0.1:1/postgres?" +
				"sslmode=disable&connect_timeout=2) does not answer: ",
			"start the server that NANO_GEN_DATABASE_URL names, with a user that may create databases, " +
				"or unset it to start a container"},
	} {
		dir, p := cachedModule(t)
		_, err := Generate(Options{Dir: dir, Postgres: p, Fresh: true, DatabaseURL: tc.url})
		problems := gen.Errors(err)
		if gen.ExitCode(err) != 3 || len(problems) != 1 || problems[0].Fix != tc.fix ||
			!strings.Contains(problems[0].Msg, "-fresh ignores nanogen.sum, and "+tc.msg) ||
			strings.Contains(problems[0].Msg, "s3cret") {
			t.Errorf("%s: exit %d, error:\n%v", tc.url, gen.ExitCode(err), err)
		}
	}
}

// Source problems of the declaration exit 1, before any database.
func TestDeclarationErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string, p *project.Postgres)
		want   string
	}{
		{"no name", func(_ *testing.T, _ string, p *project.Postgres) { p.Name = "" }, "Postgres.Name is empty\n" +
			"  fix: set Postgres.Name, the directory under internal/db/gen, such as the database name"},
		{"no migrations directory", func(_ *testing.T, _ string, p *project.Postgres) { p.Migrations = "db/migrations" },
			"db/migrations: the migrations directory does not exist\n" +
				"  fix: create the directory, or set Postgres.Migrations to the directory of the migrations"},
		{"no migration file", func(t *testing.T, dir string, _ *project.Postgres) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, "migrations", testMigration)); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "migrations/README.md", "not a migration\n")
		}, "migrations: the migrations directory holds no migration file\n" +
			"  fix: add a migration such as 000001_init.up.sql, or set Postgres.Migrations to the directory " +
			"that holds them"},
	} {
		dir, p := cachedModule(t)
		tc.change(t, dir, &p)
		_, err := Generate(Options{Dir: dir, Postgres: p, Database: noDatabase(t)})
		if err == nil || err.Error() != tc.want || gen.ExitCode(err) != 1 {
			t.Errorf("%s: error\n%v\nwant\n%s", tc.name, err, tc.want)
		}
	}
}

// nanogen.sum parses back to what it was written from, and Planned lists it
// and every output it names.
func TestSumRoundTrip(t *testing.T) {
	t.Parallel()
	dir, p := cachedModule(t)
	out := filepath.Join(dir, "internal/db/gen/app")
	s, err := readSum(out)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, SumFile))
	if !bytes.Equal(s.bytes(), data) {
		t.Fatalf("round trip:\n%s\nwant\n%s", s.bytes(), data)
	}
	want := []string{filepath.Join(out, SumFile), filepath.Join(out, testModel), filepath.Join(out, testTable)}
	if got := Planned(dir, p); !slices.Equal(got, want) {
		t.Fatalf("Planned = %v, want %v", got, want)
	}
}

// Run deletes a nano gen db file that the run did not produce, such as the
// file of a dropped table; -check reports it. A file without the header,
// written by hand, stays.
func TestRunDeletesOrphans(t *testing.T) {
	t.Parallel()
	dir, p := cachedModule(t)
	l, _ := newLayout(dir, p)
	files, err := Generate(Options{Dir: dir, Postgres: p, Database: noDatabase(t)})
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "internal/db/gen/app/public/model/dropped.go", header+"package model\n")
	write(t, dir, "internal/db/gen/app/public/model/hand.go", "package model\n")
	orphans := orphansOf(l, files)
	if want := []string{filepath.Join(l.out, "public/model/dropped.go")}; !slices.Equal(orphans, want) {
		t.Fatalf("orphans = %v, want %v", orphans, want)
	}
}

// Main needs -name, and reports usage problems with exit 2.
func TestMainUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := Main(nil, &stdout, &stderr); code != 2 || stderr.String() != "nano gen db: -name is required\n" {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if code := Main([]string{"-no-such-flag"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d for an unknown flag", code)
	}
}

// inputChange names the first difference.
func TestInputChange(t *testing.T) {
	t.Parallel()
	in := func(kv ...string) []line {
		var out []line
		for i := 0; i < len(kv); i += 2 {
			out = append(out, line{kind: "input", key: kv[i], value: kv[i+1]})
		}
		return out
	}
	base := in("image", "a", "migration 1.up.sql", "x")
	for _, tc := range []struct {
		now  []line
		want string
	}{
		{base, ""},
		{in("image", "b", "migration 1.up.sql", "x"), "image changed from a to b"},
		{in("image", "a", "migration 1.up.sql", "y"), "migration 1.up.sql changed"},
		{in("image", "a"), "migration 1.up.sql was removed"},
		{in("image", "a", "migration 1.up.sql", "x", "migration 2.up.sql", "z"), "migration 2.up.sql is new"},
	} {
		if got := inputChange(base, tc.now); got != tc.want {
			t.Errorf("inputChange(%v) = %q, want %q", tc.now, got, tc.want)
		}
	}
	want := "r, and x\n  fix: start Docker, or set " + EnvDatabaseURL
	if got := unreachable("r", errors.New("x")).Error(); got != want {
		t.Errorf("unreachable wraps a plain error as %q", got)
	}
}
