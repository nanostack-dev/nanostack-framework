package keyset

import (
	"time"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
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

// The casts of the integer and float forms are per dialect. The statement's
// dialect selects the cast when it is serialized (ijet.DialectSwitch), so one
// key serves a page of any dialect. PostgreSQL also serves CockroachDB, and
// MySQL also serves MariaDB.
//
//	          integer                   float
//	PostgreSQL CAST(e AS bigint)         CAST(e AS double precision)
//	MySQL      CAST(e AS SIGNED)         CAST(e AS DOUBLE)
//	SQLite     CAST(e AS INTEGER)        CAST(e AS REAL)

func intExprKey[T any](name string, expression jet.IntegerExpression, nullable bool, value func(T) *int64) Key[T] {
	cast := ijet.IntDialectSwitch(func(d ijet.Dialect) ijet.IntegerExpression {
		var cast ijet.IntegerExpression
		switch d {
		case mysql.Dialect:
			cast = mysql.CAST(expression).AS_SIGNED()
		case sqlite.Dialect:
			cast = sqlite.CAST(expression).AS_INTEGER()
		default:
			cast = jet.CAST(expression).AS_BIGINT()
		}
		if nullable {
			return cast
		}
		return ijet.IntExp(ijet.COALESCE(cast, ijet.Int(0)))
	})
	return intKey(computedTarget(name, cast), nullable, value)
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
	cast := ijet.FloatDialectSwitch(func(d ijet.Dialect) ijet.FloatExpression {
		var cast ijet.FloatExpression
		switch d {
		case mysql.Dialect:
			cast = mysql.CAST(expression).AS_DOUBLE()
		case sqlite.Dialect:
			cast = sqlite.CAST(expression).AS_REAL()
		default:
			cast = jet.CAST(expression).AS_DOUBLE()
		}
		if nullable {
			return cast
		}
		return ijet.FloatExp(ijet.COALESCE(cast, ijet.Float(0)))
	})
	return floatKey(computedTarget(name, cast), nullable, value)
}

// TimeExprKey orders on a timestamptz expression. It reads NULL as the zero
// time.Time (0001-01-01 UTC). See IntExprKey.
//
// It is PostgreSQL only, as TimeKey is: MySQL and SQLite have no timestamp
// with time zone, so Page returns ErrDialect there.
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
	return timeKey(computedTarget(name, cast), nullable, value)
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
	return stringKey(computedTarget(name, wrapped), nullable, value)
}
