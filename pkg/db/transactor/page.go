package transactor

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/pkg/jetx"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

var (
	// ErrPageSourceMissing is returned when Run is called without From. It is
	// keyset.ErrPageSourceMissing.
	ErrPageSourceMissing = keyset.ErrPageSourceMissing

	// ErrPageOrderConflict is returned when Run is called with both OrderBy and
	// Keyset: a keyset owns the whole ordering. It is
	// keyset.ErrPageOrderConflict.
	ErrPageOrderConflict = keyset.ErrPageOrderConflict
)

// PageBuilder runs the count-then-page pair behind a search.Result, in the
// context transaction. It forwards to keyset.Page, which holds the pager.
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
// Searches order with Keyset rather than OrderBy. A keyset ends the ordering
// with a unique tiebreak, which makes the order total, and lets Run return a
// NextCursor and accept one back:
//
//	Keyset(transactor.Keys(input.Sort, map[workspace.SortField]transactor.Key[model.Workspaces]{
//		workspace.SortFieldCreatedAt: transactor.TimeKey(table.Workspaces.CreatedAt,
//			func(m model.Workspaces) time.Time { return m.CreatedAt }),
//	}, transactor.StringKey(table.Workspaces.ID, func(m model.Workspaces) string { return m.ID })))
//
// Both type parameters are inferred from mapFunc; neither is spelled at a call
// site.
type PageBuilder[T any, R any] struct {
	db      qrm.DB
	columns []jet.Projection
	from    jet.ReadableTable
	where   jet.BoolExpression
	orderBy []jet.OrderByClause
	keyset  *Order[T]
	mapFunc func(T) R
}

// Page starts a paginated query returning rows of T mapped to R. It forwards
// to keyset.Page when Run resolves the executor.
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

// Keyset orders the page by a total keyset ordering built with Keys or
// StrictKeys. It replaces OrderBy, and enables cursor pagination: Run accepts
// Pagination.Cursor and fills Result.NextCursor. See keyset.PageBuilder.Keyset
// for the rows T may hold.
func (b *PageBuilder[T, R]) Keyset(order Order[T]) *PageBuilder[T, R] {
	b.keyset = &order
	return b
}

// Run counts the matches and fetches one page, with the context transaction
// when there is one. It is keyset.PageBuilder.Run: the count runs first, Total
// counts every match, and a cursor follows the rules written there.
//
// A page never locks rows: under a requested row lock (ForShare, ForUpdate,
// WithRowLock and the others), Run returns ErrRowLockSearch and runs nothing.
func (b *PageBuilder[T, R]) Run(
	ctx context.Context, pagination search.Pagination,
) Result[search.Result[R]] {
	if rowLockRequested(ctx) {
		return newResult(search.Result[R]{}, ErrRowLockSearch)
	}
	page := keyset.Page(Executor(ctx, b.db), b.mapFunc, b.columns...).
		From(b.from).
		Where(b.where).
		OrderBy(b.orderBy...)
	if b.keyset != nil {
		page = page.Keyset(*b.keyset)
	}
	result, err := page.Run(ctx, toDBPagination(pagination))
	return newResult(fromDBResult(result), err)
}

// SortColumns translates a search request's sort fields into order-by clauses
// using a field-to-column map.
//
// It replaces the switch each repository wrote to map its sort enum onto
// columns: a mapping is data, and as a map a missing case cannot fall through
// silently. Fields absent from the map are ignored, so an enum value with no
// column simply does not sort.
//
// It is a free function because the clauses it returns depend on no builder:
// they suit OrderBy on a PageBuilder and on any other statement.
func SortColumns[S ~string](sorts []search.Sort[S], columns map[S]jet.Column) []jet.OrderByClause {
	clauses := make([]jet.OrderByClause, 0, len(sorts))
	for _, sort := range sorts {
		if column, ok := columns[sort.Field]; ok {
			clauses = append(clauses, jetx.OrderBy(column, sort.Direction))
		}
	}
	return clauses
}
