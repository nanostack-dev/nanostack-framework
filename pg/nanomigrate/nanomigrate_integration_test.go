//go:build integration

package nanomigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// A migration can outlast the App's start timeout, and a process can be
// stopped mid migration. Only a real database shows whether schema_migrations
// ends up clean in both cases. The server starts in a container (see
// pg/internal/pgtest):
//
//	go test -tags=integration ./pg/nanomigrate/

func writeMigrations(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func migrationState(t *testing.T, db *sql.DB) (int, bool) {
	t.Helper()
	var version int
	var dirty bool
	if err := db.QueryRowContext(t.Context(), `SELECT version, dirty FROM schema_migrations`).
		Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return version, dirty
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

// The generated Build runs nano.Invoke functions before App.Start, with the
// context of nano.Main, which has no deadline. So a migration longer than
// StartTimeout ends clean, and the App then starts within its timeout.
func TestRunMigrationSlowerThanTheStartTimeoutEndsClean(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_slow.up.sql":   "SELECT pg_sleep(1); CREATE TABLE it_slow (id int);",
		"1_slow.down.sql": "DROP TABLE it_slow;",
	})
	app := nano.NewApp("migrations-test", nano.AppConfig{StartTimeout: 200 * time.Millisecond})

	startedAt := time.Now()
	if err := nanomigrate.Run(t.Context(), nanomigrate.NewMigrator(
		nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard())); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if took := time.Since(startedAt); took < time.Second {
		t.Fatalf("Run took %s; the test needs a migration longer than the start timeout", took)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("start after a 1s migration with a 200ms start timeout: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
}

// nano.Main cancels the build context on SIGINT or SIGTERM.
func TestRunShutdownFinishesTheMigrationInProgress(t *testing.T) {
	if raceEnabled {
		// golang-migrate v4.19.1 reads and writes Migrate.isGracefulStop from two
		// goroutines without synchronization (migrate.go:816 and :822) once
		// GracefulStop is used. modules/migrations has the same race.
		t.Skip("golang-migrate GracefulStop races under -race; run this test without -race")
	}
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_slow.up.sql":     "SELECT pg_sleep(1); CREATE TABLE it_first (id int);",
		"1_slow.down.sql":   "DROP TABLE it_first;",
		"2_second.up.sql":   "CREATE TABLE it_second (id int);",
		"2_second.down.sql": "DROP TABLE it_second;",
	})
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(300*time.Millisecond, cancel)

	err := nanomigrate.Run(
		ctx,
		nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard()),
	)

	if !errors.Is(err, nanomigrate.ErrStopped) {
		t.Fatalf("Run = %v, want ErrStopped: the App must not start after a shutdown signal", err)
	}
	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
	if tableExists(t, db, "it_second") {
		t.Error("the migration after the signal ran")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_a.up.sql":   "CREATE TABLE it_a (id int);",
		"1_a.down.sql": "DROP TABLE it_a;",
	})
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard())
	for range 2 {
		if err := nanomigrate.Run(t.Context(), m); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
}

func TestRunRefusesADirtyVersionUnlessAutoFixDirty(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_a.up.sql":   "CREATE TABLE it_a (id int);",
		"1_a.down.sql": "DROP TABLE it_a;",
		"2_b.up.sql":   "CREATE TABLE it_b (id int);",
		"2_b.down.sql": "DROP TABLE it_b;",
	})
	if _, err := db.ExecContext(t.Context(),
		`CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
		 CREATE TABLE it_a (id int);
		 INSERT INTO schema_migrations VALUES (1, true);`); err != nil {
		t.Fatal(err)
	}

	err := nanomigrate.Run(
		t.Context(),
		nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard()),
	)
	if err == nil {
		t.Fatal("Run migrated over a dirty version")
	}

	fixing := nanomigrate.Config{Enabled: true, BasePath: dir, AutoFixDirty: true}
	if err = nanomigrate.Run(t.Context(), nanomigrate.NewMigrator(fixing, db, discard())); err != nil {
		t.Fatalf("Run with AutoFixDirty: %v", err)
	}
	if version, dirty := migrationState(t, db); version != 2 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (2, false)", version, dirty)
	}
}

func TestRunWithAnEmptyDirectorySucceeds(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	err := nanomigrate.Run(t.Context(), nanomigrate.NewMigrator(
		nanomigrate.Config{Enabled: true, BasePath: t.TempDir()}, db, discard()))
	if err != nil {
		t.Fatalf("Run on a directory without migrations = %v, want nil", err)
	}
}

// golang-migrate keeps the connection it migrates on until it is closed. Run
// must give it back to the pool, or each Run holds one connection of the
// service pool, and an idle PostgreSQL session, for the life of the process.
func TestRunGivesItsConnectionBack(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	db.SetMaxOpenConns(2)
	dir := writeMigrations(t, map[string]string{
		"1_a.up.sql":   "CREATE TABLE it_conn (id int);",
		"1_a.down.sql": "DROP TABLE it_conn;",
	})
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, Validate: true, BasePath: dir}, db, discard())
	for i := range 3 {
		if err := nanomigrate.Run(t.Context(), m); err != nil {
			t.Fatalf("Run %d: %v", i+1, err)
		}
		if inUse := db.Stats().InUse; inUse != 0 {
			t.Fatalf("after Run %d: %d connections in use, want 0", i+1, inUse)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping after three Runs with MaxOpenConns=2: %v", err)
	}
	// The pool stays open: Run must not close the application's *sql.DB.
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("the pool is closed after Run: %v", err)
	}
}
