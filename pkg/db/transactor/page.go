package transactor

import (
	"context"
	"errors"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// ErrPageSourceMissing is returned when Run is called without From.
var ErrPageSourceMissing = errors.New("transactor: paginated query has no FROM")

// PageBuilder runs the count-then-page pair behind a search.Result.
//
// Every paginated repository search is the same shape: count the matches, take
// one page of rows in some order, map them, and assemble a search.Result. Only
// the filter predicates and the sortable columns differ per entity. PageBuilder
// owns the rest.
//
//	return transactor.Page(r.db, r.mapper.ToDomain, table.Workspaces.AllColumns).
//		From(r.joinOrganizations()).
//		Where(whereStmt).
//		OrderBy(transactor.SortColumns(input.Sort, map[workspace.SortField]jet.Column{
//			workspace.SortFieldCreatedAt: table.Workspaces.CreatedAt,
//		})...).
//		Run(ctx, input.Pagination).
//		Value()
//
// Run returns a Result, so it terminates with Value() like every other helper
// and the On* translations compose on it.
//
// Both type parameters are inferred from mapFunc; neither is spelled at a call
// site. That is why mapFunc precedes the columns — a method cannot introduce a
// type parameter, so everything the builder needs must bind at construction.
type PageBuilder[T any, R any] struct {
	db      qrm.DB
	columns []jet.Projection
	from    jet.ReadableTable
	where   jet.BoolExpression
	orderBy []jet.OrderByClause
	mapFunc func(T) R
}

// Page starts a paginated query returning rows of T mapped to R.
func Page[T any, R any](db qrm.DB, mapFunc func(T) R, columns ...jet.Projection) *PageBuilder[T, R] {
	return &PageBuilder[T, R]{db: db, columns: columns, mapFunc: mapFunc}
}

// From sets the table or join to select from. It is required.
func (b *PageBuilder[T, R]) From(table jet.ReadableTable) *PageBuilder[T, R] {
	b.from = table
	return b
}

// Where filters both the count and the page. Passing nil selects everything.
func (b *PageBuilder[T, R]) Where(condition jet.BoolExpression) *PageBuilder[T, R] {
	b.where = condition
	return b
}

// OrderBy appends ordering to the page query. The count ignores it.
//
// Pass SortColumns to translate a search request's sort fields into clauses.
func (b *PageBuilder[T, R]) OrderBy(clauses ...jet.OrderByClause) *PageBuilder[T, R] {
	b.orderBy = append(b.orderBy, clauses...)
	return b
}

// Run counts the matches and fetches one page.
//
// The count runs first: when it is zero the page query is skipped, since there
// is nothing to fetch. Total always reflects every match, not the page.
func (b *PageBuilder[T, R]) Run(
	ctx context.Context, pagination search.Pagination,
) Result[search.Result[R]] {
	if b.from == nil {
		return newResult(search.Result[R]{}, ErrPageSourceMissing)
	}

	total, err := QueryCount(ctx, b.db, b.countStatement()).Value()
	if err != nil {
		return newResult(search.Result[R]{}, err)
	}
	if total == 0 {
		return newResult(search.Result[R]{Items: []R{}, Total: 0, Count: 0}, nil)
	}

	items, err := QueryMapSlice(ctx, b.db, b.pageStatement(pagination), b.mapFunc).Value()
	if err != nil {
		return newResult(search.Result[R]{}, err)
	}
	return newResult(search.Result[R]{Items: items, Total: total, Count: len(items)}, nil)
}

func (b *PageBuilder[T, R]) countStatement() jet.Statement {
	stmt := b.from.SELECT(jet.COUNT(jet.STAR))
	if b.where != nil {
		stmt = stmt.WHERE(b.where)
	}
	return stmt
}

func (b *PageBuilder[T, R]) pageStatement(pagination search.Pagination) jet.Statement {
	stmt := b.from.SELECT(b.columns[0], b.columns[1:]...)
	if b.where != nil {
		stmt = stmt.WHERE(b.where)
	}
	if len(b.orderBy) > 0 {
		stmt = stmt.ORDER_BY(b.orderBy...)
	}
	return stmt.LIMIT(int64(pagination.Limit)).OFFSET(int64(pagination.Offset))
}

// SortColumns translates a search request's sort fields into order-by clauses
// using a field-to-column map.
//
// It replaces the switch each repository wrote to map its sort enum onto
// columns: a mapping is data, and as a map a missing case cannot fall through
// silently. Fields absent from the map are ignored, so an enum value with no
// column simply does not sort.
//
// It is a free function because it introduces the sort-field type, and a method
// cannot declare type parameters.
func SortColumns[S ~string](sorts []search.Sort[S], columns map[S]jet.Column) []jet.OrderByClause {
	clauses := make([]jet.OrderByClause, 0, len(sorts))
	for _, sort := range sorts {
		if column, ok := columns[sort.Field]; ok {
			clauses = append(clauses, orderBy(column, sort.Direction))
		}
	}
	return clauses
}

// orderBy is deliberately unexported: pkg/jetx.OrderBy is the exported helper
// for one-off ordering, and duplicating it here would give the framework two.
func orderBy(column jet.Column, direction search.SortDirection) jet.OrderByClause {
	if direction == search.SortDescending {
		return column.DESC()
	}
	return column.ASC()
}
