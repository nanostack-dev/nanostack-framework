package transactor

import (
	"context"
	"database/sql"

	"github.com/go-jet/jet/v2/qrm"
)

type txContextKey struct{}

// Transactor runs work inside a SQL transaction and propagates that transaction via context.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type sqlTransactor struct {
	db *sql.DB
}

// New creates a transaction runner backed by the provided database handle.
func New(db *sql.DB) Transactor {
	return &sqlTransactor{db: db}
}

func (t *sqlTransactor) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	ctxWithTx := WithTx(ctx, tx)
	if err := fn(ctxWithTx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// WithTx returns a context that carries tx.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, txContextKey{}, tx)
}

// CurrentTx returns the transaction stored in ctx when present.
func CurrentTx(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(txContextKey{}).(*sql.Tx)
	return tx
}

// Executor returns the context transaction when present, otherwise db.
func Executor(ctx context.Context, db qrm.DB) qrm.DB {
	if tx := CurrentTx(ctx); tx != nil {
		return tx
	}
	return db
}
