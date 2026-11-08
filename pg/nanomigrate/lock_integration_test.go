//go:build integration

package nanomigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4/database"

	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// holdMigrationLock takes the migration lock on a connection of its own, as
// a peer replica that migrates does, and returns its release.
func holdMigrationLock(t *testing.T, db *sql.DB) (peerPID int, release func()) {
	t.Helper()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var dbName, schema string
	if err = conn.QueryRowContext(t.Context(),
		"SELECT current_database(), current_schema(), pg_backend_pid()").Scan(&dbName, &schema, &peerPID); err != nil {
		t.Fatal(err)
	}
	id, err := database.GenerateAdvisoryLockId(dbName, schema, nanomigrate.MigrationsTable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(t.Context(), "SELECT pg_advisory_lock($1)", id); err != nil {
		t.Fatal(err)
	}
	release = sync.OnceFunc(func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", id)
		_ = conn.Close()
	})
	t.Cleanup(release)
	return peerPID, release
}

func advisoryLocksBesides(t *testing.T, db *sql.DB, pid int) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(
		t.Context(),
		"SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted AND pid <> $1",
		pid,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A replica that waits for the migration lock of another one must stop on
// SIGTERM (the build context), not when the other migration ends.
func TestRunStopsWhileAnotherInstanceHoldsTheLock(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_a.up.sql":   "CREATE TABLE it_locked (id int);",
		"1_a.down.sql": "DROP TABLE it_locked;",
	})
	peer, _ := holdMigrationLock(t, db)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(300*time.Millisecond, cancel)

	startedAt := time.Now()
	err := nanomigrate.Run(
		ctx,
		nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard()),
	)

	if !errors.Is(err, nanomigrate.ErrStopped) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want ErrStopped over context.Canceled", err)
	}
	if took := time.Since(startedAt); took > 3*time.Second {
		t.Fatalf("Run took %s after the stop; it must not wait for the peer's lock", took)
	}
	if tableExists(t, db, "it_locked") {
		t.Fatal("the migration ran without the lock")
	}
	if n := advisoryLocksBesides(t, db, peer); n != 0 {
		t.Fatalf("%d advisory locks held besides the peer's, want 0: a session kept the lock", n)
	}
	if inUse := db.Stats().InUse; inUse != 1 {
		t.Fatalf("%d connections in use, want only the peer's", inUse)
	}
}

func TestRunMigratesOnceThePeerReleasesTheLock(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	dir := writeMigrations(t, map[string]string{
		"1_a.up.sql":   "CREATE TABLE it_after_peer (id int);",
		"1_a.down.sql": "DROP TABLE it_after_peer;",
	})
	_, release := holdMigrationLock(t, db)
	time.AfterFunc(500*time.Millisecond, release)

	startedAt := time.Now()
	if err := nanomigrate.Run(t.Context(),
		nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: dir}, db, discard())); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if took := time.Since(startedAt); took < 500*time.Millisecond {
		t.Fatalf("Run took %s, want it to wait for the peer's lock", took)
	}
	if version, dirty := migrationState(t, db); version != 1 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty=%v), want (1, false)", version, dirty)
	}
	if n := advisoryLocksBesides(t, db, -1); n != 0 {
		t.Fatalf("%d advisory locks held after Run, want 0", n)
	}
}
