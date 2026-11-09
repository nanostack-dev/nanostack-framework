package keyset

import (
	"time"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// A computed key orders on an expression, not on a column: for example a
// correlated count of a widget's requests, or the time of its last request.
// name is the key's name in the cursor signature and its select alias. The
// page selects the expression AS name, so the field of T that the alias maps
// to holds the value that the cursor carries. Name the field's alias after
// the row, for example "widget_row.request_count" for the RequestCount field
// of a WidgetRow. Do not select the expression yourself: PageBuilder adds
// Order.Projections to the page, and a hand-written query adds them too.
//
// name is written into the SQL, so it must match ^[a-z_][a-z0-9_.]*$. A
// constructor panics otherwise.
//
// An expression that reads only NOT NULL columns can still be NULL: a scalar
// subquery that finds no row, such as the MAX of a widget with no requests.
// The scan would put the zero value into a non-pointer field, the cursor
// would carry it, and the rows after it would drop out of the walk. Use the
// Nullable form for such an expression, or wrap it in COALESCE.
//
// The integer, float and time forms cast the expression to the SQL type of
// their cursor value (bigint, double precision, timestamp with time zone).
// The order, the seek and the select then share one type, and the scan
// always fits the field. PostgreSQL drops a cast to the type the expression
// already has. The text forms do not cast: a cast to text would change the
// order of a citext or collated expression.
//
// A computed key writes PostgreSQL SQL. A source of another dialect is
// ErrDialect.

// IntExprKey orders on a NOT NULL integer expression, for example a COUNT.
func IntExprKey[T any](name string, expression jet.IntegerExpression, value func(T) int64) Key[T] {
	return intExprKey(name, expression, false, ptrOf(value))
}

// NullableIntExprKey orders on an integer expression that can be NULL.
func NullableIntExprKey[T any](name string, expression jet.IntegerExpression, value func(T) *int64) Key[T] {
	return intExprKey(name, expression, true, value)
}

func intExprKey[T any](name string, expression jet.IntegerExpression, nullable bool, value func(T) *int64) Key[T] {
	cast := jet.IntExp(jet.CAST(expression).AS_BIGINT())
	return intKey(computedTarget(name, cast), cast, nullable, value)
}

// FloatExprKey orders on a NOT NULL double precision expression.
func FloatExprKey[T any](name string, expression jet.FloatExpression, value func(T) float64) Key[T] {
	return floatExprKey(name, expression, false, ptrOf(value))
}

// NullableFloatExprKey orders on a double precision expression that can be
// NULL, for example an AVG over no rows.
func NullableFloatExprKey[T any](name string, expression jet.FloatExpression, value func(T) *float64) Key[T] {
	return floatExprKey(name, expression, true, value)
}

func floatExprKey[T any](name string, expression jet.FloatExpression, nullable bool, value func(T) *float64) Key[T] {
	cast := jet.FloatExp(jet.CAST(expression).AS_DOUBLE())
	return floatKey(computedTarget(name, cast), cast, nullable, value)
}

// TimeExprKey orders on a NOT NULL timestamptz expression.
func TimeExprKey[T any](name string, expression jet.TimestampzExpression, value func(T) time.Time) Key[T] {
	return timeExprKey(name, expression, false, ptrOf(value))
}

// NullableTimeExprKey orders on a timestamptz expression that can be NULL,
// for example the MAX of a widget with no requests.
func NullableTimeExprKey[T any](
	name string, expression jet.TimestampzExpression, value func(T) *time.Time,
) Key[T] {
	return timeExprKey(name, expression, true, value)
}

func timeExprKey[T any](
	name string, expression jet.TimestampzExpression, nullable bool, value func(T) *time.Time,
) Key[T] {
	cast := jet.TimestampzExp(jet.CAST(expression).AS_TIMESTAMPZ())
	return timeKey(computedTarget(name, cast), cast, nullable, value)
}

// StringExprKey orders on a NOT NULL text expression.
func StringExprKey[T any](name string, expression jet.StringExpression, value func(T) string) Key[T] {
	return stringExprKey(name, expression, false, ptrOf(value))
}

// NullableStringExprKey orders on a text expression that can be NULL.
func NullableStringExprKey[T any](name string, expression jet.StringExpression, value func(T) *string) Key[T] {
	return stringExprKey(name, expression, true, value)
}

func stringExprKey[T any](name string, expression jet.StringExpression, nullable bool, value func(T) *string) Key[T] {
	// The ORDER BY and the select list tell an expression to leave out its
	// parentheses, and a subquery, alone or inside a function such as
	// COALESCE, is then not SQL. The cast of the other forms puts the
	// expression back in parentheses. Here a custom expression does.
	wrapped := jet.StringExp(jet.CustomExpression(jet.Token("("), expression, jet.Token(")")))
	return stringKey(computedTarget(name, wrapped), wrapped, nullable, value)
}
