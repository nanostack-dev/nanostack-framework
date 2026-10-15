package transactor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/jetx"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// Key is one column of a keyset ordering over scanned rows of T: the column
// to order and compare on, and how to read that column's value from a row.
//
// Build keys with the typed constructors (StringKey, EnumKey, TimeKey,
// LocalTimeKey, IntKey, FloatKey and their Nullable variants) so the cursor
// value and the SQL literal share one type.
type Key[T any] struct {
	column   jet.Column
	nullable bool
	// value reads the row's value, nil for SQL NULL.
	value func(T) any
	// bound decodes a cursor value into the comparisons against it. A JSON
	// null decodes to a bound with isNull set.
	bound func(json.RawMessage) (keyBound, error)
}

// keyBound holds the comparisons of one column against one cursor value.
type keyBound struct {
	isNull  bool
	literal jet.Expression
	gt      jet.BoolExpression
	lt      jet.BoolExpression
	eq      jet.BoolExpression
}

func newKey[T any, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	literal func(V) jet.Expression,
	compare func(V) (gt, lt, eq jet.BoolExpression),
) Key[T] {
	return Key[T]{
		column:   column,
		nullable: nullable,
		value: func(row T) any {
			if v := value(row); v != nil {
				return *v
			}
			return nil
		},
		bound: func(raw json.RawMessage) (keyBound, error) {
			if string(raw) == "null" {
				if !nullable {
					return keyBound{}, fmt.Errorf("%w: null for non-null column %s", search.ErrInvalidCursor, column.Name())
				}
				return keyBound{isNull: true, eq: column.IS_NULL()}, nil
			}
			var v V
			if err := json.Unmarshal(raw, &v); err != nil {
				return keyBound{}, fmt.Errorf("%w: value for column %s", search.ErrInvalidCursor, column.Name())
			}
			gt, lt, eq := compare(v)
			return keyBound{literal: literal(v), gt: gt, lt: lt, eq: eq}, nil
		},
	}
}

func ptrOf[T any, V any](value func(T) V) func(T) *V {
	return func(row T) *V {
		v := value(row)
		return &v
	}
}

// StringKey orders on a NOT NULL text column.
func StringKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return stringKey(column, false, ptrOf(value))
}

// NullableStringKey orders on a nullable text column.
func NullableStringKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return stringKey(column, true, value)
}

func stringKey[T any](column jet.ColumnString, nullable bool, value func(T) *string) Key[T] {
	return newKey(column, nullable, value,
		func(v string) jet.Expression { return jet.String(v) },
		func(v string) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.String(v)), column.LT(jet.String(v)), column.EQ(jet.String(v))
		})
}

// EnumKey orders on a NOT NULL Postgres enum column. Postgres orders an enum
// by declaration order and refuses to compare it with text, so the cursor value
// is cast to enumType, the enum's SQL type name.
func EnumKey[T any](column jet.ColumnString, enumType string, value func(T) string) Key[T] {
	return enumKey(column, enumType, false, ptrOf(value))
}

// NullableEnumKey orders on a nullable Postgres enum column.
func NullableEnumKey[T any](column jet.ColumnString, enumType string, value func(T) *string) Key[T] {
	return enumKey(column, enumType, true, value)
}

func enumKey[T any](column jet.ColumnString, enumType string, nullable bool, value func(T) *string) Key[T] {
	typed := func(v string) jet.StringExpression {
		return jet.StringExp(jet.CAST(jet.String(v)).AS(enumType))
	}
	return newKey(column, nullable, value,
		func(v string) jet.Expression { return typed(v) },
		func(v string) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
}

// TimeKey orders on a NOT NULL timestamptz column.
func TimeKey[T any](column jet.ColumnTimestampz, value func(T) time.Time) Key[T] {
	return timeKey(column, false, ptrOf(value))
}

// NullableTimeKey orders on a nullable timestamptz column.
func NullableTimeKey[T any](column jet.ColumnTimestampz, value func(T) *time.Time) Key[T] {
	return timeKey(column, true, value)
}

func timeKey[T any](column jet.ColumnTimestampz, nullable bool, value func(T) *time.Time) Key[T] {
	return newKey(column, nullable, value,
		func(v time.Time) jet.Expression { return jet.TimestampzT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampzT(v)), column.LT(jet.TimestampzT(v)), column.EQ(jet.TimestampzT(v))
		})
}

// LocalTimeKey orders on a NOT NULL timestamp (without time zone) column.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return localTimeKey(column, false, ptrOf(value))
}

// NullableLocalTimeKey orders on a nullable timestamp (without time zone) column.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return localTimeKey(column, true, value)
}

func localTimeKey[T any](column jet.ColumnTimestamp, nullable bool, value func(T) *time.Time) Key[T] {
	return newKey(column, nullable, value,
		func(v time.Time) jet.Expression { return jet.TimestampT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampT(v)), column.LT(jet.TimestampT(v)), column.EQ(jet.TimestampT(v))
		})
}

// IntKey orders on a NOT NULL integer column.
func IntKey[T any](column jet.ColumnInteger, value func(T) int64) Key[T] {
	return intKey(column, false, ptrOf(value))
}

// NullableIntKey orders on a nullable integer column.
func NullableIntKey[T any](column jet.ColumnInteger, value func(T) *int64) Key[T] {
	return intKey(column, true, value)
}

func intKey[T any](column jet.ColumnInteger, nullable bool, value func(T) *int64) Key[T] {
	return newKey(column, nullable, value,
		func(v int64) jet.Expression { return jet.Int(v) },
		func(v int64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.Int(v)), column.LT(jet.Int(v)), column.EQ(jet.Int(v))
		})
}

// FloatKey orders on a NOT NULL float or numeric column.
func FloatKey[T any](column jet.ColumnFloat, value func(T) float64) Key[T] {
	return floatKey(column, false, ptrOf(value))
}

// NullableFloatKey orders on a nullable float or numeric column.
func NullableFloatKey[T any](column jet.ColumnFloat, value func(T) *float64) Key[T] {
	return floatKey(column, true, value)
}

func floatKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float64) Key[T] {
	return newKey(column, nullable, value,
		func(v float64) jet.Expression { return jet.Float(v) },
		func(v float64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.Float(v)), column.LT(jet.Float(v)), column.EQ(jet.Float(v))
		})
}

type orderedKey[T any] struct {
	key       Key[T]
	direction search.SortDirection
}

// Order is a total keyset ordering over rows of T: the requested sort columns
// followed by a unique tiebreak column. Because the tiebreak is unique, every
// row has exactly one position, so a cursor (the last row's values) splits the
// result into the rows before it and the rows after it with no skips or
// repeats, however rows are inserted or deleted between pages.
//
// Build one with Keys and hand it to PageBuilder.Keyset. A repository whose
// query does not fit PageBuilder uses OrderBy, Seek and Trim directly.
type Order[T any] struct {
	keys      []orderedKey[T]
	signature string
}

// Keys builds the keyset ordering for a search's sort fields.
//
// columns maps each sortable field to its key; fields absent from the map are
// ignored, as with SortColumns. The tiebreak keys must together be unique and
// NOT NULL within the rows the search can return, normally the primary key.
// A table with a composite key passes every column the search scope does not
// already fix, for example the member id for a search scoped to one
// organization, or both columns otherwise. The tiebreak sorts in the direction
// of the last requested field (ascending when none), so a composite index on
// (sort, id) serves the query in one direction scan. A column requested twice
// keeps its first position, and a tiebreak column is not repeated when it was
// requested.
func Keys[S ~string, T any](sorts []search.Sort[S], columns map[S]Key[T], tiebreak Key[T], more ...Key[T]) Order[T] {
	order := Order[T]{}
	seen := map[string]bool{}
	direction := search.SortAscending
	for _, sort := range sorts {
		key, ok := columns[sort.Field]
		if !ok {
			continue
		}
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		direction = normalizeDirection(sort.Direction)
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	for _, key := range append([]Key[T]{tiebreak}, more...) {
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	parts := make([]string, len(order.keys))
	for i, k := range order.keys {
		parts[i] = qualifiedName(k.key.column) + ":" + string(k.direction)
	}
	order.signature = strings.Join(parts, ",")
	return order
}

func normalizeDirection(direction search.SortDirection) search.SortDirection {
	if direction == search.SortDescending {
		return search.SortDescending
	}
	return search.SortAscending
}

func qualifiedName(column jet.Column) string {
	if table := column.TableName(); table != "" {
		return table + "." + column.Name()
	}
	return column.Name()
}

// Signature identifies the ordering, for example
// "widgets.created_at:DESC,widgets.id:DESC". Cursors embed it.
func (o Order[T]) Signature() string {
	return o.signature
}

// OrderBy returns the ORDER BY clauses, tiebreak included. Postgres defaults
// apply to NULLs: last when ascending, first when descending. Seek relies on
// exactly that placement.
func (o Order[T]) OrderBy() []jet.OrderByClause {
	clauses := make([]jet.OrderByClause, len(o.keys))
	for i, k := range o.keys {
		clauses[i] = jetx.OrderBy(k.key.column, k.direction)
	}
	return clauses
}

// Seek returns the predicate selecting the rows strictly after cursor in this
// ordering, or nil for an empty cursor. A cursor issued for another ordering,
// or one that does not decode, fails with search.ErrInvalidCursor.
func (o Order[T]) Seek(cursor string) (jet.BoolExpression, error) {
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
		if bounds[i], err = k.key.bound(decoded.Values[i]); err != nil {
			return nil, err
		}
	}
	if row := o.rowSeek(bounds); row != nil {
		return row, nil
	}
	return o.expandedSeek(bounds), nil
}

// rowSeek uses a row-value comparison, which Postgres can answer with one
// range scan of a matching composite index. It only applies when every key is
// NOT NULL and all keys share a direction; otherwise it returns nil.
func (o Order[T]) rowSeek(bounds []keyBound) jet.BoolExpression {
	direction := o.keys[0].direction
	columns := make([]jet.Expression, len(o.keys))
	literals := make([]jet.Expression, len(o.keys))
	for i, k := range o.keys {
		if k.key.nullable || k.direction != direction {
			return nil
		}
		columns[i] = k.key.column
		literals[i] = bounds[i].literal
	}
	if direction == search.SortDescending {
		return jet.ROW(columns...).LT(jet.ROW(literals...))
	}
	return jet.ROW(columns...).GT(jet.ROW(literals...))
}

// expandedSeek spells the comparison out key by key:
//
//	(k1 after v1) OR (k1 = v1 AND k2 after v2) OR ... (... AND id after vid)
//
// "after" follows the ORDER BY placement of NULLs: ascending sorts them last,
// so every non-null value has NULLs after it and nothing follows a NULL;
// descending sorts them first, so a NULL is followed by every non-null value.
func (o Order[T]) expandedSeek(bounds []keyBound) jet.BoolExpression {
	var disjuncts []jet.BoolExpression
	var prefix []jet.BoolExpression
	for i, k := range o.keys {
		if after := afterBound(k, bounds[i]); after != nil {
			disjuncts = append(disjuncts, jetx.CombineFilters(append(append([]jet.BoolExpression{}, prefix...), after)...))
		}
		prefix = append(prefix, bounds[i].eq)
	}
	if len(disjuncts) == 0 {
		// Only reachable when every value is a trailing ascending NULL, which a
		// unique NOT NULL tiebreak rules out; select nothing rather than all.
		return jet.Bool(false)
	}
	return jetx.CombineFiltersWithOr(disjuncts...)
}

func afterBound[T any](k orderedKey[T], bound keyBound) jet.BoolExpression {
	column := k.key.column
	if k.direction == search.SortDescending {
		if bound.isNull {
			return column.IS_NOT_NULL()
		}
		return bound.lt
	}
	if bound.isNull {
		return nil
	}
	if k.key.nullable {
		return bound.gt.OR(column.IS_NULL())
	}
	return bound.gt
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
// row is present it returns the cursor of the page's last row; otherwise the
// page is the end and the cursor is empty.
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
