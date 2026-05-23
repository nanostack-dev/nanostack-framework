package jetx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

type CountResult struct {
	Count int64 `alias:"count"`
}

// DBOptions allows repository methods to receive a transaction explicitly.
type DBOptions struct {
	Tx *sql.Tx
}

// Executor chooses the transaction from options when present, checks context for implicit transaction, otherwise returns db.
func Executor(ctx context.Context, db qrm.DB, options *DBOptions) qrm.DB {
	if options != nil && options.Tx != nil {
		return options.Tx
	}
	if tx := transactor.CurrentTx(ctx); tx != nil {
		return tx
	}
	return db
}

func ToStringExpressionSliceMap[T any](slice []T, f func(T) string) []jet.Expression {
	result := make([]jet.Expression, 0, len(slice))
	for _, value := range slice {
		result = append(result, jet.String(f(value)))
	}
	return result
}

func ToStringExpressions[T any](slice []T) []jet.Expression {
	result := make([]jet.Expression, len(slice))
	for i, value := range slice {
		result[i] = jet.String(fmt.Sprintf("%v", value))
	}
	return result
}

func Query[T any](ctx context.Context, db qrm.DB, stmt jet.Statement, options *DBOptions) (T, error) {
	var result T
	err := stmt.QueryContext(ctx, Executor(ctx, db, options), &result)
	return result, err
}

func QueryOptional[T any](ctx context.Context, db qrm.DB, stmt jet.Statement, options *DBOptions) (*T, error) {
	result, err := Query[T](ctx, db, stmt, options)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, qrm.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

func QueryMap[T any, R any](
	ctx context.Context,
	db qrm.DB,
	stmt jet.Statement,
	mapFunc func(T) R,
	options *DBOptions,
) (R, error) {
	result, err := Query[T](ctx, db, stmt, options)
	if err != nil {
		var zero R
		return zero, err
	}
	return mapFunc(result), nil
}

func QueryOptionalMap[T any, R any](
	ctx context.Context,
	db qrm.DB,
	stmt jet.Statement,
	mapFunc func(T) R,
	options *DBOptions,
) (*R, error) {
	result, err := QueryOptional[T](ctx, db, stmt, options)
	if err != nil || result == nil {
		return nil, err
	}
	mapped := mapFunc(*result)
	return &mapped, nil
}

func QueryMapSlice[T any, R any](
	ctx context.Context,
	db qrm.DB,
	stmt jet.Statement,
	mapFunc func(T) R,
	options *DBOptions,
) ([]R, error) {
	var results []T
	if err := stmt.QueryContext(ctx, Executor(ctx, db, options), &results); err != nil {
		return nil, err
	}
	mapped := make([]R, len(results))
	for i, result := range results {
		mapped[i] = mapFunc(result)
	}
	return mapped, nil
}

func Exec(ctx context.Context, db qrm.DB, stmt jet.Statement, options *DBOptions) error {
	_, err := stmt.ExecContext(ctx, Executor(ctx, db, options))
	return err
}

func QueryCount(ctx context.Context, db qrm.DB, table jet.Table, options *DBOptions) (int64, error) {
	statement := table.SELECT(jet.COUNT(jet.STAR).AS("count_result.count"))
	return QueryCountWithStatement(ctx, db, statement, options)
}

func QueryCountWithBoolExpression(
	ctx context.Context,
	db qrm.DB,
	table jet.Table,
	expr jet.BoolExpression,
	options *DBOptions,
) (int64, error) {
	statement := table.SELECT(jet.COUNT(jet.STAR).AS("count_result.count")).WHERE(expr)
	return QueryCountWithStatement(ctx, db, statement, options)
}

func QueryCountWithStatement(ctx context.Context, db qrm.DB, statement jet.Statement, options *DBOptions) (int64, error) {
	var result CountResult
	if err := statement.QueryContext(ctx, Executor(ctx, db, options), &result); err != nil {
		return 0, err
	}
	return result.Count, nil
}

func WithTx(db qrm.DB, fn func(tx *sql.Tx) error) error {
	if tx, ok := db.(*sql.Tx); ok {
		return fn(tx)
	}
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return errors.New("db is not a *sql.DB or *sql.Tx")
	}
	tx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func WithTxReturn[T any](db qrm.DB, fn func(tx *sql.Tx) (T, error)) (T, error) {
	var value T
	err := WithTx(db, func(tx *sql.Tx) error {
		result, err := fn(tx)
		value = result
		return err
	})
	return value, err
}

func OrderBy(column jet.Column, direction search.SortDirection) jet.OrderByClause {
	switch direction {
	case search.SortDescending:
		return column.DESC()
	default:
		return column.ASC()
	}
}

// FilterBuilder provides utilities for building Jet search filters.
type FilterBuilder struct{}

func NewFilterBuilder() FilterBuilder {
	return FilterBuilder{}
}

func (fb FilterBuilder) BuildIDFilter(column jet.ColumnString, ids []string) jet.BoolExpression {
	return fb.BuildStringArrayFilter(column, ids)
}

func (fb FilterBuilder) BuildStringArrayFilter(column jet.ColumnString, values []string) jet.BoolExpression {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return column.EQ(jet.String(values[0]))
	}
	return column.IN(ToStringExpressions(values)...)
}

func (fb FilterBuilder) BuildFullTextSearchFilter(columns []jet.ColumnString, term string) jet.BoolExpression {
	if term == "" || len(columns) == 0 {
		return nil
	}
	pattern := jet.String("%" + term + "%")
	conditions := make([]jet.BoolExpression, 0, len(columns))
	for _, column := range columns {
		conditions = append(conditions, column.LIKE(pattern))
	}
	return fb.CombineFiltersWithOr(conditions...)
}

func (fb FilterBuilder) BuildDateRangeFilter(column jet.ColumnTimestampz, from, to *time.Time) jet.BoolExpression {
	var conditions []jet.BoolExpression
	if from != nil {
		conditions = append(conditions, column.GT_EQ(jet.TimestampzT(*from)))
	}
	if to != nil {
		conditions = append(conditions, column.LT_EQ(jet.TimestampzT(*to)))
	}
	return fb.CombineFilters(conditions...)
}

func (fb FilterBuilder) CombineFilters(filters ...jet.BoolExpression) jet.BoolExpression {
	valid := compactBoolExpressions(filters)
	if len(valid) == 0 {
		return nil
	}
	result := valid[0]
	for i := 1; i < len(valid); i++ {
		result = result.AND(valid[i])
	}
	return result
}

func (fb FilterBuilder) CombineFiltersWithOr(filters ...jet.BoolExpression) jet.BoolExpression {
	valid := compactBoolExpressions(filters)
	if len(valid) == 0 {
		return nil
	}
	result := valid[0]
	for i := 1; i < len(valid); i++ {
		result = result.OR(valid[i])
	}
	return result
}

func compactBoolExpressions(filters []jet.BoolExpression) []jet.BoolExpression {
	valid := make([]jet.BoolExpression, 0, len(filters))
	for _, filter := range filters {
		if filter != nil {
			valid = append(valid, filter)
		}
	}
	return valid
}
