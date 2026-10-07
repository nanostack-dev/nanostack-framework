package transactor

import (
	"context"
	"errors"

	jet "github.com/go-jet/jet/v2/postgres"
)

type rowLockContextKey struct{}

var (
	// ErrRowLockOutsideTx is returned when a locked query runs without a
	// transaction: the lock would be released as the statement ends.
	ErrRowLockOutsideTx = errors.New("transactor: row lock requested outside a transaction")
	// ErrRowLockNotSelect is returned when a locked context runs a statement
	// that is not a SELECT, which has no row lock clause.
	ErrRowLockNotSelect = errors.New("transactor: row lock requested on a statement that is not a SELECT")
)

// ForShare asks every query run with the returned context to lock the rows it
// reads FOR SHARE, until the transaction ends. Pass it inline, to the one call
// whose rows must not change underneath the transaction:
//
//	template, err := templates.Find(transactor.ForShare(txCtx), id)
func ForShare(ctx context.Context) context.Context {
	return WithRowLock(ctx, jet.SHARE())
}

// ForUpdate is ForShare with FOR UPDATE: the rows are read to be written, and
// no other transaction may lock them in any mode until this one ends.
func ForUpdate(ctx context.Context) context.Context {
	return WithRowLock(ctx, jet.UPDATE())
}

// WithRowLock asks for any row lock clause go-jet can build, such as
// jet.UPDATE().NOWAIT() or jet.UPDATE().SKIP_LOCKED().
func WithRowLock(ctx context.Context, lock jet.RowLock) context.Context {
	return context.WithValue(ctx, rowLockContextKey{}, lock)
}

func withRequestedRowLock(ctx context.Context, stmt jet.Statement) (jet.Statement, error) {
	lock, requested := ctx.Value(rowLockContextKey{}).(jet.RowLock)
	if !requested {
		return stmt, nil
	}
	if CurrentTx(ctx) == nil {
		return nil, ErrRowLockOutsideTx
	}
	selectStmt, isSelect := stmt.(jet.SelectStatement)
	if !isSelect {
		return nil, ErrRowLockNotSelect
	}
	return selectStmt.FOR(lock), nil
}
