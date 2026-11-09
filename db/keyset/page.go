package keyset

import (
	"context"
	"fmt"
	"slices"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Source is the table or join a page reads. It is a ReadableTable of
// db/postgres today. The dialect packages have no common table interface
// (INNER_JOIN returns a different type in each), so Run resolves Source with
// a type switch. Any other value, a table of db/mysql or db/sqlite included,
// is ErrDialect.
type Source = any

// PageBuilder runs the count-then-page pair behind a search.Result.
//
// Every paginated search has the same shape: count the matches, take one page
// of rows in some order, map them, and assemble a search.Result. Only the
// filter and the sort keys change per entity. PageBuilder owns the rest.
//
//	order, err := keyset.OrderOf(input.Sort, map[workspace.SortField]keyset.Key[model.Workspaces]{
//		workspace.SortFieldCreatedAt: keyset.TimeKey(table.Workspaces.CreatedAt,
//			func(m model.Workspaces) time.Time { return m.CreatedAt }),
//	}, keyset.StringKey(table.Workspaces.ID, func(m model.Workspaces) string { return m.ID }))
//	if err != nil {
//		return search.Result[workspace.Workspace]{}, err
//	}
//	return keyset.Page(db, toDomain, table.Workspaces.AllColumns).
//		From(table.Workspaces).
//		Where(table.Workspaces.OrganizationID.EQ(jet.String(organizationID))).
//		RequireWhere().
//		Keyset(order).
//		Run(ctx, input.Pagination)
type PageBuilder[T, R any] struct {
	db           query.Executor
	columns      []jet.Projection
	source       Source
	where        jet.BoolExpression
	requireWhere bool
	orderBy      []jet.OrderByClause
	keyset       *Order[T]
	mapRow       func(T) R
	mapPage      func(context.Context, []T) ([]R, error)
}

// Page starts a paginated query that returns rows of T mapped to R. db is the
// executor of both statements: a *sql.DB, a *sql.Tx, or the transaction the
// caller took from its context. Page never reads a transaction itself.
//
// Both type parameters are inferred from mapFunc. It comes before the columns
// because they are variadic.
func Page[T, R any](db query.Executor, mapFunc func(T) R, columns ...jet.Projection) *PageBuilder[T, R] {
	return &PageBuilder[T, R]{db: db, columns: columns, mapRow: mapFunc}
}

// PageOf is Page with a mapper that reads the whole page at once, for example
// to load the children of the page's rows in one statement. The mapper gets
// the rows of the page only, never the extra row that tells whether another
// page follows. A nil result is an empty page.
func PageOf[T, R any](
	db query.Executor, mapPage func(context.Context, []T) ([]R, error), columns ...jet.Projection,
) *PageBuilder[T, R] {
	return &PageBuilder[T, R]{db: db, columns: columns, mapPage: mapPage}
}

// From sets the table or join to select from. It is required.
func (b *PageBuilder[T, R]) From(source Source) *PageBuilder[T, R] {
	b.source = source
	return b
}

// Where filters both the count and the page. nil selects everything, unless
// RequireWhere is set.
func (b *PageBuilder[T, R]) Where(condition jet.BoolExpression) *PageBuilder[T, R] {
	b.where = condition
	return b
}

// RequireWhere is search mode: Run refuses a nil Where with
// search.ErrUnscoped, so a search whose scope came out empty can never run as
// "select everything".
func (b *PageBuilder[T, R]) RequireWhere() *PageBuilder[T, R] {
	b.requireWhere = true
	return b
}

// OrderBy appends ordering to an offset page. The count ignores it. A keyset
// owns the whole ordering, so OrderBy and Keyset together are
// ErrPageOrderConflict.
func (b *PageBuilder[T, R]) OrderBy(clauses ...jet.OrderByClause) *PageBuilder[T, R] {
	b.orderBy = append(b.orderBy, clauses...)
	return b
}

// Keyset orders the page by a total keyset ordering built with OrderOf, and
// enables cursor pagination: Run accepts Pagination.Cursor and fills
// Result.NextCursor.
//
// T must scan one SQL row into one value. Run fetches limit+1 rows and reads
// the next cursor from the last kept one. So a destination that groups rows
// (a model with a nested slice of joined children), or a join that repeats a
// parent, would cut pages short and end the walk early. Page the parent rows
// flat, then load the children of the page in a second statement (PageOf).
//
// The page selects the computed keys of the ordering (Order.Projections)
// after the columns of Page, so the scan fills the row fields that the next
// cursor reads. Do not list them in the columns. The count does not select
// them.
func (b *PageBuilder[T, R]) Keyset(order Order[T]) *PageBuilder[T, R] {
	b.keyset = &order
	return b
}

// Run counts the matches and reads one page.
//
// The count runs first. When it is zero, the page query does not run. Total
// always counts every match of the filter, not the page and not only the rows
// after a cursor.
//
// With a Keyset, the page starts after Pagination.Cursor when one is given,
// and NextCursor is set whenever another row follows the page, in offset mode
// too, so a client can move from offsets to cursors at any page. A cursor
// with an offset is search.ErrCursorWithOffset, and a cursor that does not
// belong to this ordering is search.ErrInvalidCursor. Without a Keyset, a
// cursor is search.ErrInvalidCursor.
//
// Run checks the cursor with Pagination.Validate. It does not check the limit:
// a search checks it with search.CheckPage before it builds the page.
func (b *PageBuilder[T, R]) Run(ctx context.Context, page search.Pagination) (search.Result[R], error) {
	table, seek, err := b.prepare(page)
	if err != nil {
		return search.Result[R]{}, err
	}
	total, err := query.Count(ctx, b.db, b.countStatement(table))
	if err != nil {
		return search.Result[R]{}, err
	}
	if total == 0 {
		return search.Result[R]{Items: []R{}}, nil
	}
	rows, next, err := b.fetch(ctx, table, page, seek)
	if err != nil {
		return search.Result[R]{}, err
	}
	items, err := b.mapRows(ctx, rows)
	if err != nil {
		return search.Result[R]{}, err
	}
	return search.Result[R]{Items: items, Total: total, Count: len(items), NextCursor: next}, nil
}

// prepare refuses a page that cannot run, before any statement runs.
func (b *PageBuilder[T, R]) prepare(page search.Pagination) (jet.ReadableTable, jet.BoolExpression, error) {
	table, err := readable(b.source)
	if err != nil {
		return nil, nil, err
	}
	if b.keyset != nil && len(b.orderBy) > 0 {
		return nil, nil, ErrPageOrderConflict
	}
	if b.requireWhere && b.where == nil {
		return nil, nil, fmt.Errorf("%w: the page has no WHERE", search.ErrUnscoped)
	}
	if err = page.Validate(); err != nil {
		return nil, nil, err
	}
	if b.keyset == nil {
		if page.Cursor != "" {
			return nil, nil, fmt.Errorf("%w: this search has no keyset ordering", search.ErrInvalidCursor)
		}
		return table, nil, nil
	}
	seek, err := b.keyset.Seek(page.Cursor)
	if err != nil {
		return nil, nil, err
	}
	return table, seek, nil
}

// readable resolves the source to a PostgreSQL table or join.
func readable(source Source) (jet.ReadableTable, error) {
	switch table := source.(type) {
	case nil:
		return nil, ErrPageSourceMissing
	case jet.ReadableTable:
		return table, nil
	default:
		return nil, fmt.Errorf("%w: %T is not a PostgreSQL table or join", ErrDialect, source)
	}
}

// fetch reads the page rows and, with a keyset, the next cursor.
func (b *PageBuilder[T, R]) fetch(
	ctx context.Context, table jet.ReadableTable, page search.Pagination, seek jet.BoolExpression,
) ([]T, string, error) {
	if b.keyset == nil {
		rows, err := query.All[T](ctx, b.db, b.pageStatement(table, page))
		return rows, "", err
	}
	rows, err := query.All[T](ctx, b.db, b.keysetStatement(table, page, seek))
	if err != nil {
		return nil, "", err
	}
	return b.keyset.Trim(rows, int(page.Limit))
}

func (b *PageBuilder[T, R]) mapRows(ctx context.Context, rows []T) ([]R, error) {
	var items []R
	if b.mapPage != nil {
		mapped, err := b.mapPage(ctx, rows)
		if err != nil {
			return nil, err
		}
		items = mapped
	} else {
		items = functional.Seq[T](rows).Map(b.mapRow)
	}
	if items == nil {
		return []R{}, nil
	}
	return items, nil
}

func (b *PageBuilder[T, R]) countStatement(table jet.ReadableTable) jet.Statement {
	stmt := table.SELECT(jet.COUNT(jet.STAR))
	if b.where != nil {
		stmt = stmt.WHERE(b.where)
	}
	return stmt
}

func (b *PageBuilder[T, R]) pageStatement(table jet.ReadableTable, page search.Pagination) jet.Statement {
	stmt := table.SELECT(b.columns[0], b.columns[1:]...)
	if b.where != nil {
		stmt = stmt.WHERE(b.where)
	}
	if len(b.orderBy) > 0 {
		stmt = stmt.ORDER_BY(b.orderBy...)
	}
	return stmt.LIMIT(int64(page.Limit)).OFFSET(int64(page.Offset))
}

// keysetStatement fetches one row past the page, so Trim can tell whether
// another page follows without a second query.
func (b *PageBuilder[T, R]) keysetStatement(
	table jet.ReadableTable, page search.Pagination, seek jet.BoolExpression,
) jet.Statement {
	// The computed keys are selected AS their names, so the scan fills the
	// row fields that the next cursor reads.
	columns := b.columns
	if computed := b.keyset.Projections(); len(computed) > 0 {
		columns = slices.Concat(columns, []jet.Projection{computed})
	}
	stmt := table.SELECT(columns[0], columns[1:]...)
	if where := and(b.where, seek); where != nil {
		stmt = stmt.WHERE(where)
	}
	stmt = stmt.ORDER_BY(b.keyset.OrderBy()...).LIMIT(int64(page.Limit) + 1)
	if page.Offset > 0 {
		stmt = stmt.OFFSET(int64(page.Offset))
	}
	return stmt
}
