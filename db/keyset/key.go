package keyset

import (
	"encoding/json"
	"fmt"
	"regexp"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Key is one column of a keyset ordering over scanned rows of T: the column
// or computed expression to order and compare on, and how to read its value
// from a row.
//
// Build keys with the typed constructors (StringKey, UUIDKey, InetKey, EnumKey,
// TimeKey, LocalTimeKey, MySQLTimestampKey, MariaDBTimestampKey, IntKey,
// FloatKey, RealKey, the computed keys IntExprKey, FloatExprKey, TimeExprKey
// and StringExprKey, and their Nullable forms) so the cursor value and the SQL literal share one type. A
// constructor refuses a cursor value its column cannot hold with
// search.ErrInvalidCursor, before any query runs. What a column can hold
// depends on the dialect, so the check runs when Page knows the dialect of its
// source.
//
// A key takes the column of any dialect package: the column types of
// db/postgres, db/mysql and db/sqlite are the same types.
type Key[T any] struct {
	// column is the column of a column key, nil for a computed key.
	column jet.Column
	// expression is what the key orders and compares on: the column, or the
	// expression of a computed key.
	expression jet.Expression
	// computed is the name of a computed key: its name in the cursor
	// signature and its select alias. It is empty for a column key.
	computed string
	// projection is the expression AS computed, nil for a column key.
	projection jet.Projection
	nullable   bool
	nulls      nullPlacement
	// value reads the row's value, nil for SQL NULL.
	value func(T) any
	// bound decodes a cursor value into the comparisons against it on one
	// dialect. A JSON null decodes to a bound with isNull set.
	bound func(dialect, json.RawMessage) (keyBound, error)
	// only lists the dialects that have the key's column type. The zero value
	// means every dialect.
	only dialects
	// sortExpr is what ORDER BY sorts on when it is not the column itself, on
	// one dialect. nil means the column.
	sortExpr func(dialect) ijet.Expression
	// columnOrder reports whether the column's own comparison operators follow
	// the key order on a dialect. A row-value seek needs it. nil means yes.
	columnOrder func(dialect) bool
	// withSQLiteLayout rebuilds bound with another SQLite time layout. Only a
	// LocalTimeKey sets it.
	withSQLiteLayout func(layout string) func(dialect, json.RawMessage) (keyBound, error)
}

// nullPlacement says where NULLs sort. The zero value is the contract on every
// dialect: last when ascending, first when descending.
type nullPlacement int

const (
	nullsDefault nullPlacement = iota
	nullsFirst
	nullsLast
)

// NullsFirst returns the key with NULLs sorted before every value, in both
// directions. Use it when the API promises a placement that must not flip
// with the direction. PostgreSQL needs a matching index (... NULLS FIRST) to
// serve it without a sort.
func (k Key[T]) NullsFirst() Key[T] {
	k.nulls = nullsFirst
	return k
}

// NullsLast returns the key with NULLs sorted after every value, in both
// directions, for example "unlicensed organizations last".
func (k Key[T]) NullsLast() Key[T] {
	k.nulls = nullsLast
	return k
}

// nullsAfter reports whether NULLs follow every value in direction.
func (k Key[T]) nullsAfter(direction search.SortDirection) bool {
	switch k.nulls {
	case nullsFirst:
		return false
	case nullsLast:
		return true
	case nullsDefault:
		return direction != search.SortDescending
	}
	return direction != search.SortDescending
}

// supports reports ErrDialect when the key's column type does not exist on d.
func (k Key[T]) supports(d dialect) error {
	if k.only != 0 && !k.only.has(d) {
		return fmt.Errorf("%w: the key on %s does not exist on %s", ErrDialect, k.label(), d)
	}
	return nil
}

// orderExpression is what ORDER BY sorts on for d.
func (k Key[T]) orderExpression(d dialect) ijet.Expression {
	if k.sortExpr != nil {
		return k.sortExpr(d)
	}
	return k.expression
}

// comparesInOrder reports whether a row value of the column follows the key
// order on d.
func (k Key[T]) comparesInOrder(d dialect) bool {
	return k.columnOrder == nil || k.columnOrder(d)
}

// keyBound holds the comparisons of one column against one cursor value. gt
// selects the values after the cursor value in ascending order, lt the values
// before it. Either is nil when no value lies on that side (an enum's first or
// last label).
type keyBound struct {
	isNull  bool
	literal ijet.Expression
	gt      ijet.BoolExpression
	lt      ijet.BoolExpression
	eq      ijet.BoolExpression
}

// columnBound compares column with literal through the column's own
// operators.
func columnBound(column ijet.Expression, literal ijet.Expression) keyBound {
	return keyBound{
		literal: literal,
		gt:      ijet.Gt(column, literal),
		lt:      ijet.Lt(column, literal),
		eq:      ijet.Eq(column, literal),
	}
}

// keyCodec converts a column value to and from its cursor form. encode runs
// when a cursor is issued, decode when one comes back. The cursor form is the
// same on every dialect, so a walk gets the same tokens everywhere. decode
// must refuse any value no column of the key's type can hold, and accept must
// refuse what the column cannot hold on one dialect, so a forged cursor fails
// as ErrInvalidCursor and not as a database error.
type keyCodec[V any] struct {
	encode func(V) any
	decode func(json.RawMessage) (V, error)
	// accept refuses a decoded value that d cannot hold. nil accepts all.
	accept func(dialect, V) error
}

func jsonCodec[V any]() keyCodec[V] {
	return keyCodec[V]{
		encode: func(v V) any { return v },
		decode: func(raw json.RawMessage) (V, error) {
			var v V
			err := json.Unmarshal(raw, &v)
			return v, err
		},
	}
}

// boundFunc builds the comparisons of the column against one decoded value on
// one dialect.
type boundFunc[V any] func(dialect, V) keyBound

// literalBound is the boundFunc of a key that compares the column with a
// per-dialect literal.
func literalBound[V any](column ijet.Expression, literal func(dialect, V) ijet.Expression) boundFunc[V] {
	return func(d dialect, v V) keyBound {
		return columnBound(column, literal(d, v))
	}
}

// newKey is a key on a column.
func newKey[T, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	codec keyCodec[V],
	bound boundFunc[V],
) Key[T] {
	return newTargetKey(columnTarget(column), nullable, value, codec, bound)
}

// keyTarget is what a key orders on: a column, or a named expression.
type keyTarget struct {
	column     jet.Column
	expression jet.Expression
	computed   string
	projection jet.Projection
}

func columnTarget(column jet.Column) keyTarget {
	return keyTarget{column: column, expression: column}
}

// computedTarget checks name, which the pager writes into the SQL as the
// select alias.
func computedTarget(name string, expression jet.Expression) keyTarget {
	if !computedName.MatchString(name) {
		panic(fmt.Sprintf("keyset: computed key name %q is not a row.field alias (%s)", name, computedName))
	}
	return keyTarget{expression: expression, computed: name, projection: expression.AS(name)}
}

// computedName is the form of a computed key name: exactly one dot, so the
// scan maps the alias to a field (row.field). The scan splits an alias at its
// first dot, so a name with no dot or with two dots fills no field, and the
// cursor would carry the zero value.
var computedName = regexp.MustCompile(`^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*$`)

// label names the target in an error message.
func (t keyTarget) label() string {
	if t.computed != "" {
		return "computed key " + t.computed
	}
	return "column " + t.column.Name()
}

// label names the key in an error message.
func (k Key[T]) label() string {
	return keyTarget{column: k.column, computed: k.computed}.label()
}

func newTargetKey[T, V any](
	target keyTarget,
	nullable bool,
	value func(T) *V,
	codec keyCodec[V],
	bound boundFunc[V],
) Key[T] {
	return Key[T]{
		column:     target.column,
		expression: target.expression,
		computed:   target.computed,
		projection: target.projection,
		nullable:   nullable,
		value: func(row T) any {
			if v := value(row); v != nil {
				return codec.encode(*v)
			}
			return nil
		},
		bound: func(d dialect, raw json.RawMessage) (keyBound, error) {
			if string(raw) == "null" {
				if !nullable {
					return keyBound{}, fmt.Errorf(
						"%w: null for non-null %s",
						search.ErrInvalidCursor,
						target.label(),
					)
				}
				return keyBound{isNull: true, eq: target.expression.IS_NULL()}, nil
			}
			v, err := codec.decode(raw)
			if err == nil && codec.accept != nil {
				err = codec.accept(d, v)
			}
			if err != nil {
				return keyBound{}, fmt.Errorf("%w: value for %s", search.ErrInvalidCursor, target.label())
			}
			return bound(d, v), nil
		},
	}
}

// name identifies the key in the cursor signature: the column as the SQL
// references it, or the name of a computed key. A column key reads it when
// the order is built, not when the key is, so a key built before its table
// still gets the table name.
func (k Key[T]) name() string {
	if k.computed != "" {
		return k.computed
	}
	return qualifiedName(k.column)
}

// Projection returns the expression of a computed key AS its name, and nil
// for a column key. Put it in the columns of a page to fill the row field on
// every order, not only when the order uses the key: Page does not select it
// twice.
func (k Key[T]) Projection() jet.Projection {
	return k.projection
}

// identity tells two keys of one name apart: a column, or the SQL of a
// computed expression.
func (k Key[T]) identity() string {
	if k.computed == "" {
		return "column " + qualifiedName(k.column)
	}
	return "computed " + jet.SELECT(k.projection).DebugSql()
}

func ptrOf[T, V any](value func(T) V) func(T) *V {
	return func(row T) *V {
		v := value(row)
		return &v
	}
}

// identifier is the form of an enum type name, which a key writes into the
// SQL as raw text.
var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_.]*$`)

// mustIdentifier panics when name is not a plain lower-case identifier. The
// name comes from code, never from a request, so a mismatch is a programming
// error.
func mustIdentifier(kind, name string) {
	if !identifier.MatchString(name) {
		panic(fmt.Sprintf("keyset: %s %q is not an identifier (%s)", kind, name, identifier))
	}
}
