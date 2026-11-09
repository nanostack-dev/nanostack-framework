package transactor

import (
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// The keyset pager lives in db/keyset. The names below forward to it, so
// existing callers keep compiling and keep their cursors byte for byte. New
// code uses db/keyset.

// Key is keyset.Key. Use db/keyset.
type Key[T any] = keyset.Key[T]

// Order is keyset.Order. Use db/keyset.
type Order[T any] = keyset.Order[T]

// StringKey is keyset.StringKey. Use db/keyset.
func StringKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return keyset.StringKey(column, value)
}

// NullableStringKey is keyset.NullableStringKey. Use db/keyset.
func NullableStringKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return keyset.NullableStringKey(column, value)
}

// EnumKey is keyset.EnumKey. Use db/keyset.
func EnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) string) Key[T] {
	return keyset.EnumKey(column, enumType, labels, value)
}

// NullableEnumKey is keyset.NullableEnumKey. Use db/keyset.
func NullableEnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) *string) Key[T] {
	return keyset.NullableEnumKey(column, enumType, labels, value)
}

// InetKey is keyset.InetKey. Use db/keyset.
func InetKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return keyset.InetKey(column, value)
}

// NullableInetKey is keyset.NullableInetKey. Use db/keyset.
func NullableInetKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return keyset.NullableInetKey(column, value)
}

// UUIDKey is keyset.UUIDKey. Use db/keyset.
func UUIDKey[T any](column jet.ColumnString, value func(T) uuid.UUID) Key[T] {
	return keyset.UUIDKey(column, value)
}

// NullableUUIDKey is keyset.NullableUUIDKey. Use db/keyset.
func NullableUUIDKey[T any](column jet.ColumnString, value func(T) *uuid.UUID) Key[T] {
	return keyset.NullableUUIDKey(column, value)
}

// TimeKey is keyset.TimeKey. Use db/keyset.
func TimeKey[T any](column jet.ColumnTimestampz, value func(T) time.Time) Key[T] {
	return keyset.TimeKey(column, value)
}

// NullableTimeKey is keyset.NullableTimeKey. Use db/keyset.
func NullableTimeKey[T any](column jet.ColumnTimestampz, value func(T) *time.Time) Key[T] {
	return keyset.NullableTimeKey(column, value)
}

// LocalTimeKey is keyset.LocalTimeKey. Use db/keyset.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return keyset.LocalTimeKey(column, value)
}

// NullableLocalTimeKey is keyset.NullableLocalTimeKey. Use db/keyset.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return keyset.NullableLocalTimeKey(column, value)
}

// IntKey is keyset.IntKey. Use db/keyset.
func IntKey[T any](column jet.ColumnInteger, value func(T) int64) Key[T] {
	return keyset.IntKey(column, value)
}

// NullableIntKey is keyset.NullableIntKey. Use db/keyset.
func NullableIntKey[T any](column jet.ColumnInteger, value func(T) *int64) Key[T] {
	return keyset.NullableIntKey(column, value)
}

// FloatKey is keyset.FloatKey. Use db/keyset.
func FloatKey[T any](column jet.ColumnFloat, value func(T) float64) Key[T] {
	return keyset.FloatKey(column, value)
}

// NullableFloatKey is keyset.NullableFloatKey. Use db/keyset.
func NullableFloatKey[T any](column jet.ColumnFloat, value func(T) *float64) Key[T] {
	return keyset.NullableFloatKey(column, value)
}

// RealKey is keyset.RealKey. Use db/keyset.
func RealKey[T any](column jet.ColumnFloat, value func(T) float32) Key[T] {
	return keyset.RealKey(column, value)
}

// NullableRealKey is keyset.NullableRealKey. Use db/keyset.
func NullableRealKey[T any](column jet.ColumnFloat, value func(T) *float32) Key[T] {
	return keyset.NullableRealKey(column, value)
}

// Keys builds the keyset ordering of a search, and drops each sort field
// absent from columns. Use db/keyset: keyset.OrderOf refuses an unknown sort,
// as StrictKeys does. Ordering by another column than the client asked
// answers another question.
//
// Keys drops the unknown fields, then builds the order with keyset.OrderOf,
// which documents the tiebreak and the directions.
func Keys[S ~string, T any](sorts []search.Sort[S], columns map[S]Key[T], tiebreak Key[T], more ...Key[T]) Order[T] {
	known := make([]search.Sort[S], 0, len(sorts))
	for _, sort := range sorts {
		if _, ok := columns[sort.Field]; ok {
			known = append(known, sort)
		}
	}
	order, err := keyset.OrderOf(toDBSorts(known), columns, tiebreak, more...)
	if err != nil {
		// keyset.OrderOf fails only on a sort absent from columns, and the
		// loop above dropped each one.
		panic("transactor: Keys: " + err.Error())
	}
	return order
}

// StrictKeys is keyset.OrderOf for the sorts of pkg/search: a requested sort
// field absent from columns is search.ErrUnknownSort, naming it.
func StrictKeys[S ~string, T any](
	sorts []search.Sort[S], columns map[S]Key[T], tiebreak Key[T], more ...Key[T],
) (Order[T], error) {
	return keyset.OrderOf(toDBSorts(sorts), columns, tiebreak, more...)
}
