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

// What InTx does with a context that already carries a transaction is a fact
// only a real database confirms: that the inner work reads the outer one's
// uncommitted rows, and that the outer rollback takes the inner writes with it.
// A second connection would do neither.
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=itpass -e POSTGRES_DB=pgerrit postgres:16
//	PGERR_TEST_DSN="postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable" \
//		go test -tags=integration ./pkg/db/transactor/

func setupNestingSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS it_nesting`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE it_nesting (id text PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS it_nesting`) })
}

func countRows(t *testing.T, db qrmExecutor, id string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM it_nesting WHERE id = $1`, id).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

// qrmExecutor is the little of database/sql these assertions need, so the same
// helper reads through a transaction and through the pool.
type qrmExecutor interface {
	QueryRow(query string, args ...any) *sql.Row
}

func TestIntegrationInTxNesting(t *testing.T) {
	db := openDB(t)
	setupNestingSchema(t, db)
	ctx := context.Background()
	runner := transactor.New(db)

	t.Run("the inner call joins and reads the outer uncommitted row", func(t *testing.T) {
		var seenFromInner int
		err := runner.InTx(ctx, func(outerCtx context.Context) error {
			outerTx := transactor.CurrentTx(outerCtx)
			if _, execErr := outerTx.Exec(
				`INSERT INTO it_nesting (id) VALUES ('joined')`,
			); execErr != nil {
				return execErr
			}

			return runner.InTx(outerCtx, func(innerCtx context.Context) error {
				if transactor.CurrentTx(innerCtx) != outerTx {
					t.Error("the inner call did not join the outer transaction")
				}
				seenFromInner = countRows(t, transactor.CurrentTx(innerCtx), "joined")
				return nil
			})
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		if seenFromInner != 1 {
			t.Fatalf("the inner call saw %d rows, want 1", seenFromInner)
		}
		if got := countRows(t, db, "joined"); got != 1 {
			t.Fatalf("after commit the table holds %d rows, want 1", got)
		}
	})

	t.Run("the outer rollback takes the inner write with it", func(t *testing.T) {
		refused := errors.New("refused")

		err := runner.InTx(ctx, func(outerCtx context.Context) error {
			if innerErr := runner.InTx(outerCtx, func(innerCtx context.Context) error {
				_, execErr := transactor.CurrentTx(innerCtx).Exec(
					`INSERT INTO it_nesting (id) VALUES ('rolled_back')`,
				)
				return execErr
			}); innerErr != nil {
				return innerErr
			}
			return refused
		})
		if !errors.Is(err, refused) {
			t.Fatalf("InTx = %v, want %v", err, refused)
		}

		// The inner call returned nil, so a transaction of its own would have
		// committed the row before the outer one gave up.
		if got := countRows(t, db, "rolled_back"); got != 0 {
			t.Fatalf("the table holds %d rows, want 0", got)
		}
	})

	t.Run("without a transaction in context it begins one", func(t *testing.T) {
		err := runner.InTx(ctx, func(txCtx context.Context) error {
			if transactor.CurrentTx(txCtx) == nil {
				t.Error("no transaction was begun")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
	})
}
