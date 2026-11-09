//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// lockable reports whether the row of an order can be locked by another
// connection: false when a transaction holds it.
func lockable(t *testing.T, db *sql.DB, id uuid.UUID) bool {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(context.Background(), `SELECT 1 FROM "order" WHERE id = $1 FOR UPDATE NOWAIT`, id)
	var pqErr *pq.Error
	switch {
	case err == nil:
		return true
	case errors.As(err, &pqErr) && pqErr.Code == "55P03":
		return false
	}
	t.Fatalf("probe: %v", err)
	return false
}

// A derived find honours a row lock of the context; exists and count refuse
// it, as PostgreSQL refuses FOR UPDATE on an aggregate; a delete refuses it
// in the transactor. A derived write in a transaction is visible inside it and
// gone after a rollback.
func TestTransactionsAndRowLocks(t *testing.T) {
	db := openTorture(t)
	seedOrders(t, db, false)
	ctx, orders := context.Background(), NewOrders(db)
	row := idOf("o0p0w1k1")
	run := func(f func(context.Context) error) error { return transactor.New(db).InTx(ctx, f) }

	for name, read := range map[string]func(context.Context) error{
		"option": func(c context.Context) error {
			_, err := orders.FindByOrgAndProductAndWorkspaceAndID(c, orgs[0], products[0], row)
			return err
		},
		"get": func(c context.Context) error {
			_, err := orders.GetByOrgAndProductAndWorkspaceAndID(c, orgs[0], products[0], row)
			return err
		},
		"slice": func(c context.Context) error {
			_, err := orders.FindByOrgAndProductAndWorkspaceAndName(c, orgs[0], products[0], "same")
			return err
		},
	} {
		t.Run("lock "+name, func(t *testing.T) {
			err := run(func(tx context.Context) error {
				if err := read(transactor.ForUpdate(tx)); err != nil {
					return err
				}
				if lockable(t, db, row) {
					t.Error("the row is not locked")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !lockable(t, db, row) {
				t.Error("the lock outlived the transaction")
			}
		})
	}
	for name, aggregate := range map[string]func(context.Context) error{
		"count": func(c context.Context) error {
			_, err := orders.CountByOrgAndProductAndWorkspace(c, orgs[0], products[0])
			return err
		},
		"exists": func(c context.Context) error {
			_, err := orders.ExistsByOrgAndProductAndWorkspaceAndID(c, orgs[0], products[0], row)
			return err
		},
	} {
		t.Run("aggregate "+name, func(t *testing.T) {
			err := run(func(tx context.Context) error { return aggregate(transactor.ForUpdate(tx)) })
			var pqErr *pq.Error
			if !errors.As(err, &pqErr) || pqErr.Code != "0A000" {
				t.Fatalf("err = %v, want feature_not_supported (0A000)", err)
			}
		})
	}
	del := func(c context.Context) error {
		_, ierr := orders.DeleteByOrgAndProductAndWorkspaceAndID(
			workspacescope.With(c, "w1"),
			orgs[0],
			products[0],
			row,
		)
		return ierr
	}
	if err := del(transactor.ForUpdate(ctx)); !errors.Is(err, transactor.ErrRowLockOutsideTx) {
		t.Errorf("locked delete outside a transaction: %v, want ErrRowLockOutsideTx", err)
	}
	if err := run(
		func(tx context.Context) error { return del(transactor.ForUpdate(tx)) },
	); !errors.Is(
		err,
		transactor.ErrRowLockNotSelect,
	) {
		t.Errorf("locked delete in a transaction: %v, want ErrRowLockNotSelect", err)
	}
	if n := len(column(t, db, `SELECT 1 FROM "order" WHERE id = $1`, row)); n != 1 {
		t.Fatalf("a refused delete ran: %d rows", n)
	}

	// A delete in a transaction is seen inside it, and a rollback restores the row.
	errRollback := errors.New("rollback")
	err := run(func(tx context.Context) error {
		if ok, err := orders.DeleteByOrgAndProductAndWorkspaceAndID(
			workspacescope.With(tx, "w1"),
			orgs[0],
			products[0],
			row,
		); err != nil ||
			!ok {
			t.Errorf("delete: %v %v", ok, err)
		}
		if ok, err := orders.ExistsByOrgAndProductAndWorkspaceAndID(tx, orgs[0], products[0], row); err != nil || ok {
			t.Errorf("exists after the delete, in the transaction: %v %v", ok, err)
		}
		if ok, err := orders.ExistsByOrgAndProductAndWorkspaceAndID(ctx, orgs[0], products[0], row); err != nil || !ok {
			t.Errorf("exists after the delete, outside the transaction: %v %v", ok, err)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
	if ok, eErr := orders.ExistsByOrgAndProductAndWorkspaceAndID(ctx, orgs[0], products[0], row); eErr != nil || !ok {
		t.Errorf("the rollback did not restore the row: %v %v", ok, eErr)
	}
}
