// This file is Nanostack-only (not in go-jet): literal constructors that take a
// functional.Option. See db/UPSTREAM.md.

package postgres

import (
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Each XxxOpt is the Xxx constructor (or XxxT for time types) with the value replaced by
// a functional.Option. A present Option gives the same SQL and argument as Xxx. An absent
// one binds nil in the same place and keeps whatever cast Xxx has:
//
//   - most constructors cast, so None is a typed NULL (for example $1::text with nil);
//   - IntOpt and FloatOpt have no cast, like Int and Float, so None is an untyped NULL.
//     Where PostgreSQL or CockroachDB must infer the type from the argument alone (a
//     bare SELECT $1, for example), CockroachDB fails with "could not determine data
//     type of placeholder" (42P18), for None as for Int(1). Use Int64Opt or DoubleOpt
//     (cast to bigint and double precision) there.
//
// Do not compare a column with an XxxOpt in WHERE: col = NULL is never true. Leave the
// condition out instead, with db/filter.When.

// BoolOpt is Bool for an optional value.
func BoolOpt(o functional.Option[bool]) BoolExpression {
	return CAST(jet.BoolExp(jet.Literal(o))).AS_BOOL()
}

// IntOpt is Int for an optional value.
func IntOpt(o functional.Option[int64]) IntegerExpression {
	return jet.IntExp(jet.Literal(o))
}

// Int8Opt is Int8 for an optional value.
func Int8Opt(o functional.Option[int8]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_SMALLINT()
}

// Int16Opt is Int16 for an optional value.
func Int16Opt(o functional.Option[int16]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_SMALLINT()
}

// Int32Opt is Int32 for an optional value.
func Int32Opt(o functional.Option[int32]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_INTEGER()
}

// Int64Opt is Int64 for an optional value.
func Int64Opt(o functional.Option[int64]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_BIGINT()
}

// Uint8Opt is Uint8 for an optional value.
func Uint8Opt(o functional.Option[uint8]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_SMALLINT()
}

// Uint16Opt is Uint16 for an optional value.
func Uint16Opt(o functional.Option[uint16]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_INTEGER()
}

// Uint32Opt is Uint32 for an optional value.
func Uint32Opt(o functional.Option[uint32]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_BIGINT()
}

// Uint64Opt is Uint64 for an optional value.
func Uint64Opt(o functional.Option[uint64]) IntegerExpression {
	return CAST(jet.IntExp(jet.Literal(o))).AS_BIGINT()
}

// FloatOpt is Float for an optional value.
func FloatOpt(o functional.Option[float64]) FloatExpression {
	return jet.FloatExp(jet.Literal(o))
}

// RealOpt is Real for an optional value.
func RealOpt(o functional.Option[float32]) FloatExpression {
	return CAST(jet.Literal(o)).AS_REAL()
}

// DoubleOpt is Double for an optional value.
func DoubleOpt(o functional.Option[float64]) FloatExpression {
	return CAST(jet.Literal(o)).AS_DOUBLE()
}

// DecimalOpt is Decimal for an optional value.
func DecimalOpt(o functional.Option[string]) FloatExpression {
	return CAST(jet.Literal(o)).AS_DECIMAL()
}

// StringOpt is String for an optional value.
func StringOpt(o functional.Option[string]) StringExpression {
	return CAST(jet.StringExp(jet.Literal(o))).AS_TEXT()
}

// TextOpt is Text for an optional value.
func TextOpt(o functional.Option[string]) StringExpression {
	return CAST(jet.Literal(o)).AS_TEXT()
}

// CharOpt is Char for an optional value: CharOpt(20)(o).
func CharOpt(length ...int) func(functional.Option[string]) StringExpression {
	return func(o functional.Option[string]) StringExpression {
		return CAST(StringExp(jet.Literal(o))).AS_CHAR(length...)
	}
}

// VarCharOpt is VarChar for an optional value: VarCharOpt(20)(o).
func VarCharOpt(length ...int) func(functional.Option[string]) StringExpression {
	return func(o functional.Option[string]) StringExpression {
		return CAST(StringExp(jet.Literal(o))).AS_VARCHAR(length...)
	}
}

// JsonOpt is Json for an optional value.
//
//nolint:staticcheck // ST1003: the name mirrors upstream Json, as every XxxOpt mirrors Xxx.
func JsonOpt[T string | []byte](o functional.Option[T]) StringExpression {
	return StringExp(CAST(jet.Literal(o)).AS("json"))
}

// UUIDOpt is UUID for an optional value of any uuid type with a String method.
func UUIDOpt[T fmt.Stringer](o functional.Option[T]) StringExpression {
	return CAST(jet.Literal(o.Map(func(v T) string { return v.String() }))).AS_UUID()
}

// ByteaOpt is Bytea for an optional value.
func ByteaOpt[T string | []byte](o functional.Option[T]) ByteaExpression {
	return CAST(jet.Literal(o)).AS_BYTEA()
}

// DateOpt is DateT for an optional value.
func DateOpt(o functional.Option[time.Time]) DateExpression {
	return CAST(jet.DateExp(jet.Literal(o))).AS_DATE()
}

// TimeOpt is TimeT for an optional value.
func TimeOpt(o functional.Option[time.Time]) TimeExpression {
	return CAST(jet.TimeExp(jet.Literal(o))).AS_TIME()
}

// TimezOpt is TimezT for an optional value.
func TimezOpt(o functional.Option[time.Time]) TimezExpression {
	return CAST(jet.TimezExp(jet.Literal(o))).AS_TIMEZ()
}

// TimestampOpt is TimestampT for an optional value.
func TimestampOpt(o functional.Option[time.Time]) TimestampExpression {
	return CAST(jet.TimestampExp(jet.Literal(o))).AS_TIMESTAMP()
}

// TimestampzOpt is TimestampzT for an optional value.
func TimestampzOpt(o functional.Option[time.Time]) TimestampzExpression {
	return CAST(jet.TimestampzExp(jet.Literal(o))).AS_TIMESTAMPZ()
}
