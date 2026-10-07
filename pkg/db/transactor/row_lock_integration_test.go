//go:build integration

package transactor_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// Whether a lock blocks the writer it is meant to block is a fact only a real
// database confirms. A second transaction asking with NOWAIT answers "is the
// row locked" at once, instead of hanging the test.
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=itpass -e POSTGRES_DB=pgerrit postgres:16
//	PGERR_TEST_DSN="postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable" \
//		go test -tags=integration ./pkg/db/transactor/

func setupLockedSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_locked`,
		`CREATE TABLE it_locked (id text PRIMARY KEY)`,
		`INSERT INTO it_locked (id) VALUES ('a')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS it_locked`) })
}

type lockedRow struct{ ID string }

// holdRowLocked reads the row with lock in a transaction left open until the
// test ends.
func holdRowLocked(t *testing.T, db *sql.DB, lock func(context.Context) context.Context) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	ctx := lock(transactor.WithTx(context.Background(), tx))
	if _, err := transactor.Query[lockedRow](ctx, db, selectLockedRow("a")).Value(); err != nil {
		t.Fatalf("locked read: %v", err)
	}
}

// rowLockable reports whether another transaction can take mode on the row
// right now.
func rowLockable(t *testing.T, db *sql.DB, mode string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`SELECT id FROM it_locked WHERE id = 'a' FOR ` + mode + ` NOWAIT`)
	return err == nil
}

func TestIntegrationForShareLetsReadersShareAndBlocksWriters(t *testing.T) {
	db := openDB(t)
	setupLockedSchema(t, db)
	holdRowLocked(t, db, transactor.ForShare)

	if !rowLockable(t, db, "SHARE") {
		t.Fatal("a second FOR SHARE reader was blocked")
	}
	if rowLockable(t, db, "UPDATE") {
		t.Fatal("a writer took the row while it was held FOR SHARE")
	}
}

func TestIntegrationForUpdateBlocksEveryOtherLock(t *testing.T) {
	db := openDB(t)
	setupLockedSchema(t, db)
	holdRowLocked(t, db, transactor.ForUpdate)

	if rowLockable(t, db, "SHARE") {
		t.Fatal("a FOR SHARE reader took the row while it was held FOR UPDATE")
	}
}

func TestIntegrationAnUnlockedReadLocksNothing(t *testing.T) {
	db := openDB(t)
	setupLockedSchema(t, db)
	holdRowLocked(t, db, func(ctx context.Context) context.Context { return ctx })

	if !rowLockable(t, db, "UPDATE") {
		t.Fatal("a plain read inside a transaction locked the row")
	}
}

func TestIntegrationALockedQueryOutsideATransactionFails(t *testing.T) {
	db := openDB(t)
	setupLockedSchema(t, db)

	_, err := transactor.Query[lockedRow](transactor.ForShare(context.Background()), db, selectLockedRow("a")).Value()
	if !errors.Is(err, transactor.ErrRowLockOutsideTx) {
		t.Fatalf("err = %v, want ErrRowLockOutsideTx", err)
	}
}
