package keyset

import (
	"time"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// IntExprKey orders on an integer expression, for example a correlated COUNT
// of a row's children. It reads NULL as 0 (see the package doc, Computed
// keys).
func IntExprKey[T any](name string, expression jet.IntegerExpression, value func(T) int64) Key[T] {
	return intExprKey(name, expression, false, ptrOf(value))
}

// NullableIntExprKey orders on an integer expression that can be NULL, with
// NULLs placed as the key says (NullsFirst, NullsLast). See IntExprKey.
func NullableIntExprKey[T any](name string, expression jet.IntegerExpression, value func(T) *int64) Key[T] {
	return intExprKey(name, expression, true, value)
}

// postgresOnly marks a computed key as PostgreSQL only: its expression is
// PostgreSQL SQL (casts and COALESCE through db/postgres), so Page returns
// ErrDialect on MySQL and SQLite.
func postgresOnly[T any](key Key[T]) Key[T] {
	key.only = dialects(dialectPostgres)
	return key
}

func intExprKey[T any](name string, expression jet.IntegerExpression, nullable bool, value func(T) *int64) Key[T] {
	cast := jet.IntExp(jet.CAST(expression).AS_BIGINT())
	if !nullable {
		cast = jet.IntExp(jet.COALESCE(cast, jet.Int(0)))
	}
	return postgresOnly(intKey(computedTarget(name, cast), nullable, value))
}

// FloatExprKey orders on a double precision expression. It reads NULL as 0.
// See IntExprKey.
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
	if !nullable {
		cast = jet.FloatExp(jet.COALESCE(cast, jet.Float(0)))
	}
	return postgresOnly(floatKey(computedTarget(name, cast), nullable, value))
}

// TimeExprKey orders on a timestamptz expression. It reads NULL as the zero
// time.Time (0001-01-01 UTC). See IntExprKey.
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
	if !nullable {
		cast = jet.TimestampzExp(jet.COALESCE(cast, jet.TimestampzT(time.Time{})))
	}
	return postgresOnly(timeKey(computedTarget(name, cast), nullable, value))
}

// StringExprKey orders on a text expression. It reads NULL as the empty
// string. See IntExprKey.
func StringExprKey[T any](name string, expression jet.StringExpression, value func(T) string) Key[T] {
	return stringExprKey(name, expression, false, ptrOf(value))
}

// NullableStringExprKey orders on a text expression that can be NULL. See
// IntExprKey.
func NullableStringExprKey[T any](name string, expression jet.StringExpression, value func(T) *string) Key[T] {
	return stringExprKey(name, expression, true, value)
}

func stringExprKey[T any](name string, expression jet.StringExpression, nullable bool, value func(T) *string) Key[T] {
	// The ORDER BY and the select list tell an expression to leave out its
	// parentheses, and a subquery, alone or inside a function such as
	// COALESCE, is then not SQL. The cast of the other forms puts the
	// expression back in parentheses. Here a custom expression does.
	// The empty string is a bare token: a typed literal ('' with a cast to
	// text) would change the type, and so the order, of a citext expression.
	wrapped := jet.StringExp(jet.CustomExpression(jet.Token("("), expression, jet.Token(")")))
	if !nullable {
		wrapped = jet.StringExp(jet.CustomExpression(jet.Token("COALESCE(("), expression, jet.Token("), '')")))
	}
	return postgresOnly(stringKey(computedTarget(name, wrapped), nullable, value))
}
