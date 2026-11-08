// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Statement is any statement built with the postgres, mysql or sqlite packages.
type Statement = jet.Statement

// ErrTooManyRows is returned (wrapped, with the count) by One and Count when the result
// maps to more than one value.
var ErrTooManyRows = errors.New("query: statement returned more than one result")

// One runs stmt and returns the single value its result maps to.
//
//   - None: no row, or rows that map to no value (every mapped column NULL).
//   - Some: exactly one value. Rows are grouped the way qrm groups them, so one parent
//     whose nested slices spread over many rows is one value.
//   - ErrTooManyRows (wrapped, with the count): more than one value.
//
// T is a model struct, a custom destination struct or a scalar (One[int64] reads the
// first column; a NULL value is None). Results are scanned into []T, never into a
// struct, so duplicates are detected. SELECT_JSON_ARR works; SELECT_JSON_OBJ does not
// (its destination is one object, not rows: qrm panics), use stmt.QueryContext for it.
// T must not be a functional.Option.
func One[T any](ctx context.Context, db qrm.Queryable, stmt Statement) (functional.Option[T], error) {
	rows, err := scan[T](ctx, db, stmt)
	if err != nil {
		return functional.None[T](), fmt.Errorf("query.One[%s]: %w", typeName[T](), err)
	}
	switch len(rows) {
	case 0:
		return functional.None[T](), nil
	case 1:
		return functional.Some(rows[0]), nil
	default:
		return functional.None[T](), fmt.Errorf(
			"query.One[%s]: %w (%d results)",
			typeName[T](),
			ErrTooManyRows,
			len(rows),
		)
	}
}

// OneMap is One followed by Option.Map. It exists because One returns a pair, which
// cannot be chained.
func OneMap[T, R any](
	ctx context.Context,
	db qrm.Queryable,
	stmt Statement,
	f func(T) R,
) (functional.Option[R], error) {
	found, err := One[T](ctx, db, stmt)
	if err != nil {
		return functional.None[R](), err
	}
	return found.Map(f), nil
}

// All runs stmt and returns every value its result maps to. The result is never nil:
// no rows give an empty Seq. Rows that map to no value (every mapped column NULL) are
// left out. SELECT_JSON_ARR works; SELECT_JSON_OBJ does not (see One).
func All[T any](ctx context.Context, db qrm.Queryable, stmt Statement) (functional.Seq[T], error) {
	rows, err := scan[T](ctx, db, stmt)
	if err != nil {
		return functional.Seq[T]{}, fmt.Errorf("query.All[%s]: %w", typeName[T](), err)
	}
	if rows == nil {
		return functional.Seq[T]{}, nil
	}
	return functional.Seq[T](rows), nil
}

// AllMap is All followed by Seq.Map. The result is never nil.
func AllMap[T, R any](ctx context.Context, db qrm.Queryable, stmt Statement, f func(T) R) (functional.Seq[R], error) {
	all, err := All[T](ctx, db, stmt)
	if err != nil {
		return functional.Seq[R]{}, err
	}
	return all.Map(f), nil
}

// Exec runs a statement that returns no rows.
func Exec(ctx context.Context, db qrm.Executable, stmt Statement) (sql.Result, error) {
	res, err := stmt.ExecContext(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("query.Exec: %w", err)
	}
	return res, nil
}

// Count runs a statement whose single column is an integer, such as SELECT COUNT(*).
// It returns 0 when the statement yields no row (COUNT ... GROUP BY with no groups) or
// NULL, and ErrTooManyRows when it yields more than one non-NULL row.
func Count(ctx context.Context, db qrm.Queryable, stmt Statement) (int64, error) {
	rows, err := scan[int64](ctx, db, stmt)
	if err != nil {
		return 0, fmt.Errorf("query.Count: %w", err)
	}
	switch len(rows) {
	case 0:
		return 0, nil
	case 1:
		return rows[0], nil
	default:
		return 0, fmt.Errorf("query.Count: %w (%d results)", ErrTooManyRows, len(rows))
	}
}

// scan runs stmt into a []T. A no-rows error, which some Queryable wrappers return,
// is absence, not failure.
func scan[T any](ctx context.Context, db qrm.Queryable, stmt Statement) ([]T, error) {
	var rows []T
	if err := stmt.QueryContext(ctx, db, &rows); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, qrm.ErrNoRows)
}

func typeName[T any]() string {
	return reflect.TypeFor[T]().String()
}
