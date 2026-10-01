//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/modules/migrations"
)

// A migration can outlast fx's start deadline, and a container can be stopped
// mid migration. Only a real database shows whether schema_migrations ends up
// clean in both cases.
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=itpass postgres:16
//	MIGRATIONS_TEST_DSN="postgres://postgres:itpass@localhost:55432/postgres?sslmode=disable" \
//		go test -tags=integration ./modules/migrations/

func freshDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MIGRATIONS_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATIONS_TEST_DSN is not set")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := fmt.Sprintf("it_migrations_%d", time.Now().UnixNano())
	if _, createErr := admin.Exec("CREATE DATABASE " + name); createErr != nil {
		t.Fatalf("create database: %v", createErr)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})
	return db
}

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
	if err := db.QueryRow(`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return version, dirty
}

func newApp(db *sql.DB, basePath string) *fx.App {
	return fx.New(
		fx.NopLogger,
		fx.StartTimeout(200*time.Millisecond),
		fx.Supply(db, zerolog.Nop(), migrations.MigrationConfig{Enabled: true, BasePath: basePath}),
		fx.Provide(migrations.ProvideModuleMigrator),
		fx.Invoke(migrations.RunBeforeStart),
	)
}

func TestRunBeforeStart_MigrationSlowerThanTheStartTimeoutEndsClean(t *testing.T) {
	db := freshDatabase(t)
	dir := writeMigrations(t, map[string]string{
		"1_slow.up.sql":   "SELECT pg_sleep(1); CREATE TABLE it_slow (id int);",
		"1_slow.down.sql": "DROP TABLE it_slow;",
	})

	app := newApp(db, dir)
	if err := app.Err(); err != nil {
		t.Fatalf("build: %v", err)
	}
	startCtx, cancel := context.WithTimeout(context.Background(), app.StartTimeout())
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("start after a 1s migration with a 200ms start timeout: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
}

func TestRunBeforeStart_ShutdownSignalFinishesTheMigrationInProgress(t *testing.T) {
	db := freshDatabase(t)
	dir := writeMigrations(t, map[string]string{
		"1_slow.up.sql":     "SELECT pg_sleep(1); CREATE TABLE it_first (id int);",
		"1_slow.down.sql":   "DROP TABLE it_first;",
		"2_second.up.sql":   "CREATE TABLE it_second (id int);",
		"2_second.down.sql": "DROP TABLE it_second;",
	})

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	app := newApp(db, dir)
	if app.Err() == nil {
		t.Fatal("expected the build to fail: the app must not start after a shutdown signal")
	}

	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
	var secondExists bool
	if err := db.QueryRow(`SELECT to_regclass('it_second') IS NOT NULL`).Scan(&secondExists); err != nil {
		t.Fatal(err)
	}
	if secondExists {
		t.Error("the migration after the signal ran")
	}
}
