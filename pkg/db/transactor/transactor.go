package transactor

import (
	"context"
	"database/sql"
	"errors"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

type txContextKey struct{}

// Transactor runs work inside a SQL transaction and propagates that transaction
// via context.
type Transactor interface {
	// InTx runs fn in the transaction ctx already carries, and in a new one
	// otherwise. A service composing another service's write into a larger unit
	// therefore calls the same method as a service that owns the transaction,
	// and the outermost call owns the commit and the rollback.
	//
	// There is no way to ask for a transaction independent of the one in ctx.
	// Postgres has no such thing at this level anyway: a second connection
	// cannot see the first one's uncommitted rows, and blocks on the locks it
	// holds.
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
	if CurrentTx(ctx) != nil {
		return fn(ctx)
	}

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
	if fnErr := fn(ctxWithTx); fnErr != nil {
		return fnErr
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return commitErr
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

// Query executes a query and returns the results.
func Query[T any](ctx context.Context, db qrm.DB, stmt jet.Statement) Result[T] {
	var result T
	err := stmt.QueryContext(ctx, Executor(ctx, db), &result)
	return newResult(result, err)
}

// QueryOptional executes a query that may return 0 rows, in which case the
// result is absent — IsPresent is false and Err is nil — rather than an
// error. That is the point: "was there a row" and "did anything go wrong"
// are two questions, and a nilable pointer folds them into one.
//
//	result := repo.UpdateOptional(ctx, tenantID, instance)
//	if err := result.Err(); err != nil {
//		return err // a real failure
//	}
//	if !result.IsPresent() {
//		return nil // the row is gone — benign, nothing to do
//	}
//	updated := result.Value()
//
// Use FlatMap to chain a second lookup keyed by this one's value; absence or
// failure from either step ends the chain the same way.
//
//	config := transactor.QueryOptional[Run](ctx, db, runStmt).
//		FlatMap(func(run Run) functional.Option[Config] {
//			return transactor.QueryOptional[Config](ctx, db, configStmtFor(run))
//		})
func QueryOptional[T any](ctx context.Context, db qrm.DB, stmt jet.Statement) functional.Option[T] {
	var result T
	err := stmt.QueryContext(ctx, Executor(ctx, db), &result)
	if err != nil {
		if isNoRows(err) {
			return functional.None[T]()
		}
		return functional.Failed[T](err)
	}
	return functional.Some(result)
}

// QueryOptionalMap executes a query that may return 0 rows and maps the
// result when present. It is QueryOptional(...).Map(mapFunc) — Option.Map
// does the actual present/absent/error handling exactly once.
func QueryOptionalMap[T any, R any](
	ctx context.Context, db qrm.DB, stmt jet.Statement, mapFunc func(T) R,
) functional.Option[R] {
	return QueryOptional[T](ctx, db, stmt).Map(mapFunc)
}

// isNoRows reports whether err is the "statement matched zero rows" sentinel,
// in either form a caller can see it: sql.ErrNoRows from database/sql, or
// qrm.ErrNoRows from go-jet's row-mapping layer. QueryOptional and Result's
// OnNoRows both treat these as the same condition.
func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, qrm.ErrNoRows)
}

// QueryMap executes a query and maps the result.
func QueryMap[T any, R any](ctx context.Context, db qrm.DB, stmt jet.Statement, mapFunc func(T) R) Result[R] {
	result, err := Query[T](ctx, db, stmt).Value()
	if err != nil {
		var zero R
		return newResult(zero, err)
	}
	return newResult(mapFunc(result), nil)
}

// QueryMapSlice executes a query and maps a slice of results.
func QueryMapSlice[T any, R any](ctx context.Context, db qrm.DB, stmt jet.Statement, mapFunc func(T) R) Result[[]R] {
	var results []T
	if err := stmt.QueryContext(ctx, Executor(ctx, db), &results); err != nil {
		return newResult[[]R](nil, err)
	}
	mapped := make([]R, len(results))
	for i, result := range results {
		mapped[i] = mapFunc(result)
	}
	return newResult(mapped, nil)
}

// Exec executes a statement. It carries no value, so callers unwrap it with Err.
func Exec(ctx context.Context, db qrm.DB, stmt jet.Statement) Result[struct{}] {
	_, err := stmt.ExecContext(ctx, Executor(ctx, db))
	return newResult(struct{}{}, err)
}

// QueryCount executes a query count statement.
func QueryCount(ctx context.Context, db qrm.DB, statement jet.Statement) Result[int64] {
	query, args := statement.Sql()
	rows, err := Executor(ctx, db).QueryContext(ctx, query, args...)
	if err != nil {
		return newResult[int64](0, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return newResult[int64](0, rowsErr)
		}
		return newResult[int64](0, sql.ErrNoRows)
	}
	var count int64
	if scanErr := rows.Scan(&count); scanErr != nil {
		return newResult[int64](0, scanErr)
	}
	return newResult(count, nil)
}
