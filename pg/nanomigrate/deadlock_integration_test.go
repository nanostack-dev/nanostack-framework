//go:build integration

package nanomigrate_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// lockedBuffer is a log sink that the migration goroutine writes while the
// test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A session outside the migration can deadlock with CREATE INDEX
// CONCURRENTLY: the index build waits for the session's snapshot, and the
// session then asks for an AccessExclusiveLock on the table. A TimescaleDB
// retention policy does exactly that on every table its hypertable
// references; Anchor's startup failed this way on migration 000043. The test
// plays the policy job with a second session, and gives that session a long
// deadlock_timeout, so Postgres always aborts the migration.
func TestRunRerunsAMigrationAbortedAsADeadlockVictim(t *testing.T) {
	ctx := t.Context()
	db := pgtest.Open(t, pgtest.Fresh(t))
	table := map[string]string{
		"1_table.up.sql":   "CREATE TABLE it_victim (id int);",
		"1_table.down.sql": "DROP TABLE it_victim;",
	}
	if err := nanomigrate.Run(ctx, nanomigrate.NewMigrator(
		nanomigrate.Config{Enabled: true, BasePath: writeMigrations(t, table)}, db, discard())); err != nil {
		t.Fatalf("Run 1_table: %v", err)
	}
	table["2_index.up.sql"] = "CREATE INDEX CONCURRENTLY IF NOT EXISTS it_victim_id ON it_victim (id);"
	table["2_index.down.sql"] = "DROP INDEX CONCURRENTLY IF EXISTS it_victim_id;"
	dir := writeMigrations(t, table)

	other, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	for _, stmt := range []string{
		"SET deadlock_timeout = '30s'",
		"BEGIN ISOLATION LEVEL REPEATABLE READ",
		"SELECT 1", // Takes the snapshot that the index build waits for.
	} {
		if _, err = other.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	done := make(chan error, 1)
	go func() {
		done <- nanomigrate.Run(ctx, nanomigrate.NewMigrator(
			nanomigrate.Config{Enabled: true, BasePath: dir}, db, logger))
	}()

	waitForSnapshotWait(t, ctx, db)
	// Blocks behind the index build's ShareUpdateExclusiveLock until
	// Postgres aborts the build, one deadlock_timeout (1s) later.
	if _, err = other.ExecContext(ctx, "LOCK TABLE it_victim IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock it_victim: %v", err)
	}
	if _, err = other.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after the deadlock")
	}
	if err != nil {
		t.Fatalf("Run after a deadlock: %v\nlogs:\n%s", err, logs)
	}
	if !strings.Contains(logs.String(), "deadlock victim") {
		t.Fatalf("Run did not report the deadlock; the test did not deadlock:\n%s", logs)
	}
	if version, dirty := migrationState(t, db); version != 2 || dirty {
		t.Fatalf("schema_migrations = (%d, dirty %t), want (2, clean)", version, dirty)
	}
	var valid bool
	if err = db.QueryRowContext(ctx,
		`SELECT indisvalid FROM pg_index WHERE indexrelid = 'it_victim_id'::regclass`).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("it_victim_id is invalid: the rerun skipped the index the aborted build left")
	}
}

// waitForSnapshotWait returns once a session waits for another transaction
// to end: CREATE INDEX CONCURRENTLY waiting for the older snapshot.
func waitForSnapshotWait(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_locks WHERE locktype = 'virtualxid' AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("CREATE INDEX CONCURRENTLY never waited for the other session's snapshot")
}
