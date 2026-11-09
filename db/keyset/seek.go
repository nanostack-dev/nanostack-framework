package keyset

import (
	"fmt"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// Seek returns the predicate that selects the rows strictly after cursor in
// this ordering, or nil for an empty cursor. A cursor issued for another
// ordering, or one that does not decode, is search.ErrInvalidCursor. The
// predicate is PostgreSQL SQL: use it only in a db/postgres statement. Page
// writes the seek of its source's dialect itself.
func (o Order[T]) Seek(cursor string) (jet.BoolExpression, error) {
	return o.seek(dialectPostgres, cursor)
}

// seek is Seek for d. The values are checked against what d's columns can
// hold, so a value valid on one dialect can be ErrInvalidCursor on another.
func (o Order[T]) seek(d dialect, cursor string) (jet.BoolExpression, error) {
	if cursor == "" {
		return nil, nil
	}
	decoded, err := search.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	if decoded.Order != o.signature {
		return nil, fmt.Errorf("%w: issued for a different sort", search.ErrInvalidCursor)
	}
	if len(decoded.Values) != len(o.keys) {
		return nil, fmt.Errorf("%w: wrong number of values", search.ErrInvalidCursor)
	}
	bounds := make([]keyBound, len(o.keys))
	for i, k := range o.keys {
		if bounds[i], err = k.key.bound(d, decoded.Values[i]); err != nil {
			return nil, err
		}
	}
	if row := o.rowSeek(d, bounds); row != nil {
		return row, nil
	}
	return o.expandedSeek(d, bounds), nil
}

// rowSeek uses a row-value comparison, which PostgreSQL and SQLite answer
// with one range scan of a matching composite index. It applies only when
// every key is NOT NULL, all keys share a direction, and each column's own
// comparison follows its key order (not an enum on SQLite). MySQL does not
// use a range scan for a row-value inequality (its manual, "Row Constructor
// Expression Optimization"), so it always gets the expanded form. Otherwise
// rowSeek returns nil.
func (o Order[T]) rowSeek(d dialect, bounds []keyBound) jet.BoolExpression {
	if d == dialectMySQL {
		return nil
	}
	direction := o.keys[0].direction
	columns := make([]ijet.Expression, len(o.keys))
	literals := make([]ijet.Expression, len(o.keys))
	for i, k := range o.keys {
		if k.key.nullable || k.direction != direction || !k.key.comparesInOrder(d) {
			return nil
		}
		columns[i] = k.key.expression
		literals[i] = bounds[i].literal
	}
	row := jet.ROW
	if d == dialectSQLite {
		row = sqlite.ROW
	}
	if direction == search.SortDescending {
		return row(columns...).LT(row(literals...))
	}
	return row(columns...).GT(row(literals...))
}

// expandedSeek writes the comparison out key by key:
//
//	(k1 after v1) OR (k1 = v1 AND k2 after v2) OR ... (... AND id after vid)
//
// "after" follows the ORDER BY placement of NULLs. Where they sort last
// (ascending by default, or NullsLast), every non-null value has NULLs after
// it, and only NULLs follow a NULL. Where they sort first (descending by
// default, or NullsFirst), a NULL is followed by every non-null value.
func (o Order[T]) expandedSeek(d dialect, bounds []keyBound) jet.BoolExpression {
	var disjuncts []jet.BoolExpression
	var prefix []jet.BoolExpression
	for i, k := range o.keys {
		if after := afterBound(k, bounds[i]); after != nil {
			disjuncts = append(disjuncts, and(append(append([]jet.BoolExpression{}, prefix...), after)...))
		}
		prefix = append(prefix, bounds[i].eq)
	}
	if len(disjuncts) == 0 {
		// Only reachable when every value is a trailing ascending NULL, which a
		// unique NOT NULL tiebreak rules out. Select nothing, never everything.
		// db/postgres writes a cast to boolean, which MySQL cannot parse.
		if d == dialectPostgres {
			return jet.Bool(false)
		}
		return ijet.Bool(false)
	}
	return or(disjuncts...)
}

func afterBound[T any](k orderedKey[T], bound keyBound) jet.BoolExpression {
	column := k.key.expression
	nullsAfter := k.key.nullsAfter(k.direction)
	if bound.isNull {
		if nullsAfter {
			// Only NULLs follow a NULL; the later keys order them.
			return nil
		}
		return column.IS_NOT_NULL()
	}
	after := bound.gt
	if k.direction == search.SortDescending {
		after = bound.lt
	}
	if k.key.nullable && nullsAfter {
		// or keeps a nil side out: an enum's last label has no later label.
		return or(after, column.IS_NULL())
	}
	return after
}

// and joins the conditions that are not nil with AND, left to right. It
// returns nil when every condition is nil.
func and(conditions ...jet.BoolExpression) jet.BoolExpression {
	var joined jet.BoolExpression
	for _, condition := range conditions {
		switch {
		case condition == nil:
		case joined == nil:
			joined = condition
		default:
			joined = joined.AND(condition)
		}
	}
	return joined
}

// or is and with OR.
func or(conditions ...jet.BoolExpression) jet.BoolExpression {
	var joined jet.BoolExpression
	for _, condition := range conditions {
		switch {
		case condition == nil:
		case joined == nil:
			joined = condition
		default:
			joined = joined.OR(condition)
		}
	}
	return joined
}

// Cursor encodes the position of row, the last row of a page.
func (o Order[T]) Cursor(row T) (string, error) {
	values := make([]any, len(o.keys))
	for i, k := range o.keys {
		values[i] = k.key.value(row)
	}
	return search.EncodeCursor(o.signature, values)
}

// Trim cuts rows fetched with LIMIT limit+1 down to one page. When the extra
// row is present, it returns the cursor of the page's last row. Otherwise the
// page is the end, and the cursor is empty.
func (o Order[T]) Trim(rows []T, limit int) ([]T, string, error) {
	if limit <= 0 || len(rows) <= limit {
		return rows, "", nil
	}
	page := rows[:limit]
	next, err := o.Cursor(page[limit-1])
	if err != nil {
		return nil, "", err
	}
	return page, next, nil
}
