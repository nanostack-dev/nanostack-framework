package transactor

import (
	"context"
	"database/sql"
	"errors"

	jet "github.com/go-jet/jet/v2/postgres"
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
// result value is nil and the error is nil.
func QueryOptional[T any](ctx context.Context, db qrm.DB, stmt jet.Statement) Result[*T] {
	var result T
	err := stmt.QueryContext(ctx, Executor(ctx, db), &result)
	if err != nil {
		if isNoRows(err) {
			return newResult[*T](nil, nil)
		}
		return newResult[*T](nil, err)
	}
	return newResult(&result, nil)
}

// QueryOptionalResult is QueryOptional's Optional-based counterpart: the same
// "0 rows is not an error" semantics, but through IsPresent/Err/Value instead
// of a pointer the caller must remember to nil-check.
func QueryOptionalResult[T any](ctx context.Context, db qrm.DB, stmt jet.Statement) Optional[T] {
	result, err := QueryOptional[T](ctx, db, stmt).Value()
	if err != nil {
		return Optional[T]{err: err}
	}
	if result == nil {
		return Optional[T]{}
	}
	return Optional[T]{v: *result, present: true}
}

// QueryOptionalResultMap is QueryOptionalResult with a value mapper, mirroring
// QueryOptionalMap. It is QueryOptionalResult().Map(mapFunc) — Optional.Map
// does the actual present/absent/error handling exactly once.
func QueryOptionalResultMap[T any, R any](
	ctx context.Context, db qrm.DB, stmt jet.Statement, mapFunc func(T) R,
) Optional[R] {
	return QueryOptionalResult[T](ctx, db, stmt).Map(mapFunc)
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

// QueryOptionalMap executes a query that may return 0 rows and maps the result when present.
func QueryOptionalMap[T any, R any](ctx context.Context, db qrm.DB, stmt jet.Statement, mapFunc func(T) R) Result[*R] {
	result, err := QueryOptional[T](ctx, db, stmt).Value()
	if err != nil || result == nil {
		return newResult[*R](nil, err)
	}
	mapped := mapFunc(*result)
	return newResult(&mapped, nil)
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
