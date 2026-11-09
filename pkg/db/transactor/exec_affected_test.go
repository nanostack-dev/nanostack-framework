package transactor_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// countingDB is a qrm.DB whose ExecContext reports a fixed number of
// affected rows, or fails; it records whether a statement reached it.
type countingDB struct {
	affected int64
	err      error
	ran      bool
}

func (d *countingDB) Exec(query string, args ...any) (sql.Result, error) {
	return d.ExecContext(context.Background(), query, args...)
}

func (d *countingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.ran = true
	if d.err != nil {
		return nil, d.err
	}
	return driver.RowsAffected(d.affected), nil
}

func (d *countingDB) Query(string, ...any) (*sql.Rows, error) { return nil, errors.New("not a query") }

func (d *countingDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("not a query")
}

func deleteLockedRow() postgres.Statement {
	return lockedTable.DELETE().WHERE(lockedID.EQ(postgres.String("a")))
}

func TestExecAffectedReturnsTheAffectedRows(t *testing.T) {
	db := &countingDB{affected: 3}
	n, err := transactor.ExecAffected(context.Background(), db, deleteLockedRow()).Value()
	if err != nil || n != 3 {
		t.Fatalf("got %d, %v; want 3, nil", n, err)
	}
}

func TestExecAffectedPassesTheDriverErrorThrough(t *testing.T) {
	boom := errors.New("boom")
	db := &countingDB{err: boom}
	n, err := transactor.ExecAffected(context.Background(), db, deleteLockedRow()).Value()
	if !errors.Is(err, boom) || n != 0 {
		t.Fatalf("got %d, %v; want 0, boom", n, err)
	}
}

// A row lock on a write is refused before the statement runs, as Exec does:
// outside a transaction the lock would end with the statement, and inside one
// a DELETE has no lock clause.
func TestExecAffectedRefusesARowLock(t *testing.T) {
	for name, test := range map[string]struct {
		ctx  context.Context
		want error
	}{
		"outside a transaction": {transactor.ForUpdate(context.Background()), transactor.ErrRowLockOutsideTx},
		"in a transaction":      {transactor.ForUpdate(inFakeTx(context.Background())), transactor.ErrRowLockNotSelect},
	} {
		t.Run(name, func(t *testing.T) {
			db := &countingDB{affected: 1}
			_, err := transactor.ExecAffected(test.ctx, db, deleteLockedRow()).Value()
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			if db.ran {
				t.Fatal("the statement ran despite the refused lock")
			}
		})
	}
}
