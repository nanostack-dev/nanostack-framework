// This file is Nanostack-only (not in go-jet): literal constructors that take a
// functional.Option. See db/UPSTREAM.md.

package sqlite

import (
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Each XxxOpt is the Xxx constructor with the value replaced by a functional.Option. A
// present Option gives the same SQL and argument as Xxx; an absent one binds NULL in the
// same place.
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

// SQLite has no time types, so these format the time the way Date, Time and DateTime
// format their components. The SQL then does not depend on how the driver formats a
// time.Time.
//
// The time is formatted in UTC. A time.Time written through VALUES, SET or MODEL is
// stored with its offset (2024-01-02 03:04:05+05:00), and SQLite's DATE, TIME and
// DATETIME normalise such a value to UTC. Formatting in UTC makes DateTimeOpt(Some(t))
// equal DATETIME(column) for the same t whatever its location, and makes DateOpt give
// the UTC date. A time in the local zone of a non-UTC host is therefore shifted to UTC,
// not written as its wall clock.
const (
	sqliteDateLayout     = "2006-01-02"
	sqliteTimeLayout     = "15:04:05.999999999"
	sqliteDateTimeLayout = "2006-01-02 15:04:05.999999999"
)

// DateOpt is DATE(?) over an optional time, formatted in UTC as 2006-01-02. Absent is DATE(NULL), which is NULL.
func DateOpt(o functional.Option[time.Time]) DateExpression {
	return DATE(jet.Literal(o.Map(func(t time.Time) string { return t.UTC().Format(sqliteDateLayout) })))
}

// TimeOpt is TIME(?) over an optional time, formatted in UTC as 15:04:05.999999999.
func TimeOpt(o functional.Option[time.Time]) TimeExpression {
	return TIME(jet.Literal(o.Map(func(t time.Time) string { return t.UTC().Format(sqliteTimeLayout) })))
}

// DateTimeOpt is DATETIME(?) over an optional time, formatted in UTC as 2006-01-02 15:04:05.999999999.
func DateTimeOpt(o functional.Option[time.Time]) DateTimeExpression {
	return DATETIME(jet.Literal(o.Map(func(t time.Time) string { return t.UTC().Format(sqliteDateTimeLayout) })))
}
