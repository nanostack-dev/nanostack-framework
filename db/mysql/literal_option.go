// This file is Nanostack-only (not in go-jet): literal constructors that take a
// functional.Option. See db/UPSTREAM.md.

package mysql

import (
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Each XxxOpt is the Xxx constructor (or XxxT for time types) with the value replaced by
// a functional.Option. A present Option gives the same SQL and argument as Xxx; an absent
// one binds NULL in the same place (with the same CAST, where Xxx has one).
//
// Do not compare a column with an XxxOpt in WHERE: col = NULL is never true. Leave the
// condition out instead, with db/filter.When.

// BoolOpt is Bool for an optional value.
func BoolOpt(o functional.Option[bool]) BoolExpression { return jet.BoolExp(jet.Literal(o)) }

// IntOpt is Int for an optional value.
func IntOpt(o functional.Option[int64]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Int8Opt is Int8 for an optional value.
func Int8Opt(o functional.Option[int8]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Int16Opt is Int16 for an optional value.
func Int16Opt(o functional.Option[int16]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Int32Opt is Int32 for an optional value.
func Int32Opt(o functional.Option[int32]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Int64Opt is Int64 for an optional value.
func Int64Opt(o functional.Option[int64]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Uint8Opt is Uint8 for an optional value.
func Uint8Opt(o functional.Option[uint8]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Uint16Opt is Uint16 for an optional value.
func Uint16Opt(o functional.Option[uint16]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Uint32Opt is Uint32 for an optional value.
func Uint32Opt(o functional.Option[uint32]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// Uint64Opt is Uint64 for an optional value.
func Uint64Opt(o functional.Option[uint64]) IntegerExpression { return jet.IntExp(jet.Literal(o)) }

// FloatOpt is Float for an optional value.
func FloatOpt(o functional.Option[float64]) FloatExpression { return jet.FloatExp(jet.Literal(o)) }

// DecimalOpt is Decimal for an optional value.
func DecimalOpt(o functional.Option[string]) FloatExpression { return jet.FloatExp(jet.Literal(o)) }

// StringOpt is String for an optional value.
func StringOpt(o functional.Option[string]) StringExpression { return jet.StringExp(jet.Literal(o)) }

// UUIDOpt is UUID for an optional value of any uuid type with a String method.
func UUIDOpt[T fmt.Stringer](o functional.Option[T]) StringExpression {
	return jet.StringExp(jet.Literal(o.Map(func(v T) string { return v.String() })))
}

// BlobOpt is Blob for an optional value.
func BlobOpt(o functional.Option[[]byte]) BlobExpression { return BlobExp(jet.Literal(o)) }

// DateOpt is DateT for an optional value.
func DateOpt(o functional.Option[time.Time]) DateExpression {
	return CAST(jet.DateExp(jet.Literal(o))).AS_DATE()
}

// TimeOpt is TimeT for an optional value.
func TimeOpt(o functional.Option[time.Time]) TimeExpression {
	return CAST(jet.TimeExp(jet.Literal(o))).AS_TIME()
}

// DateTimeOpt is DateTimeT for an optional value.
func DateTimeOpt(o functional.Option[time.Time]) DateTimeExpression {
	return CAST(jet.TimestampExp(jet.Literal(o))).AS_DATETIME()
}

// TimestampOpt is TimestampT for an optional value.
func TimestampOpt(o functional.Option[time.Time]) TimestampExpression {
	return TIMESTAMP(StringExp(jet.TimestampExp(jet.Literal(o))))
}
