package keyset

import (
	"encoding/json"
	"errors"
	"time"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// TimeKey orders on a NOT NULL PostgreSQL timestamptz column. MySQL and
// SQLite have no such type (their generated models map timestamp and datetime
// columns to a timestamp without time zone): Page returns ErrDialect there.
// Use LocalTimeKey or MySQLTimestampKey.
func TimeKey[T any](column jet.ColumnTimestampz, value func(T) time.Time) Key[T] {
	return timeKey(column, false, ptrOf(value))
}

// NullableTimeKey orders on a nullable timestamptz column.
func NullableTimeKey[T any](column jet.ColumnTimestampz, value func(T) *time.Time) Key[T] {
	return timeKey(column, true, value)
}

// The ranges of the time columns, at microsecond precision.
var (
	// PostgreSQL stores timestamps from 4714-11-24 BC to 294276-12-31 AD.
	postgresMinTime = time.Date(-4713, time.November, 24, 0, 0, 0, 0, time.UTC)
	postgresMaxTime = time.Date(294276, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)
	// MySQL DATETIME holds 1000-01-01 to 9999-12-31.
	datetimeMinTime = time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC)
	datetimeMaxTime = time.Date(9999, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)
	// MySQL TIMESTAMP holds 1970-01-01 00:00:01 to 2038-01-19 03:14:07 UTC.
	timestampMinTime = time.Date(1970, time.January, 1, 0, 0, 1, 0, time.UTC)
	timestampMaxTime = time.Date(2038, time.January, 19, 3, 14, 7, 999_999_000, time.UTC)
	// SQLite compares the driver's text form, which sorts in time order for
	// four-digit years only.
	sqliteMinTime = time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)
	sqliteMaxTime = datetimeMaxTime
	// postgresEpochUnix is 2000-01-01 UTC, the PostgreSQL timestamp epoch.
	postgresEpochUnix = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
)

// microsCodec encodes an instant as microseconds since 2000-01-01 UTC, the
// form PostgreSQL itself stores. It is exact across the whole PostgreSQL
// range, and it is the cursor form on every dialect. Unix microseconds
// overflow past year 294247, RFC 3339 loses the seconds of an offset and
// cannot write years past 9999, and a decoded value carries no zone that the
// database could refuse. The arithmetic goes through seconds, because a
// time.Duration overflows after 292 years.
//
// decode refuses an instant outside the PostgreSQL range. accept refuses one
// outside [low, high] on MySQL and SQLite, when the key sets a range there.
func microsCodec() keyCodec[time.Time] {
	return keyCodec[time.Time]{
		encode: func(v time.Time) any {
			return (v.Unix()-postgresEpochUnix)*1_000_000 + int64(v.Nanosecond()/1_000)
		},
		decode: func(raw json.RawMessage) (time.Time, error) {
			var micros int64
			if err := json.Unmarshal(raw, &micros); err != nil {
				return time.Time{}, err
			}
			seconds, remainder := micros/1_000_000, micros%1_000_000
			if remainder < 0 {
				seconds, remainder = seconds-1, remainder+1_000_000
			}
			decoded := time.Unix(postgresEpochUnix+seconds, remainder*1_000).UTC()
			if decoded.Before(postgresMinTime) || decoded.After(postgresMaxTime) {
				return time.Time{}, errors.New("timestamp outside the PostgreSQL range")
			}
			return decoded, nil
		},
	}
}

func timeKey[T any](column jet.ColumnTimestampz, nullable bool, value func(T) *time.Time) Key[T] {
	key := newKey(
		column,
		nullable,
		value,
		microsCodec(),
		literalBound(column, func(_ dialect, v time.Time) ijet.Expression {
			return jet.TimestampzT(v)
		}),
	)
	key.only = dialects(dialectPostgres)
	return key
}

// LocalTimeKey orders on a NOT NULL timestamp without time zone: a PostgreSQL
// timestamp, a MySQL DATETIME, or a SQLite DATETIME column. The value is the
// wall clock, read as if it were UTC, so the cursor cannot shift it.
//
// On MySQL a cursor value outside the DATETIME range (1000-01-01 to
// 9999-12-31) is search.ErrInvalidCursor, and the literal is the wall clock as
// text, so the driver's loc setting cannot shift it. A MySQL TIMESTAMP column
// takes MySQLTimestampKey.
//
// SQLite has no time type: the driver writes a time as text in the form
// "2006-01-02 15:04:05.999999999-07:00", and the column compares as text. The
// literal is the cursor's time in that form with a +00:00 offset, so the rows
// must be written in UTC. The cursor holds microseconds, so the rows must not
// carry a finer fraction. That text sorts in time order for four-digit years
// only, so a cursor value outside the years 0000 to 9999 is
// search.ErrInvalidCursor.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return localTimeKey(column, false, ptrOf(value), datetimeMinTime, datetimeMaxTime)
}

// NullableLocalTimeKey orders on a nullable timestamp without time zone.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return localTimeKey(column, true, value, datetimeMinTime, datetimeMaxTime)
}

// MySQLTimestampKey orders on a NOT NULL MySQL or MariaDB TIMESTAMP column. A
// cursor value outside the TIMESTAMP range (1970-01-01 00:00:01 to 2038-01-19
// 03:14:07) is search.ErrInvalidCursor. MySQL reads and compares a TIMESTAMP
// in the session time zone, and the range is checked in UTC, so run the
// session in UTC: in a zone with daylight saving time, the repeated hour maps
// two instants to one wall clock, and a walk through it can skip or repeat
// rows. PostgreSQL and SQLite have no such type: Page returns ErrDialect.
func MySQLTimestampKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	key := localTimeKey(column, false, ptrOf(value), timestampMinTime, timestampMaxTime)
	key.only = dialects(dialectMySQL)
	return key
}

// NullableMySQLTimestampKey orders on a nullable MySQL TIMESTAMP column.
func NullableMySQLTimestampKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	key := localTimeKey(column, true, value, timestampMinTime, timestampMaxTime)
	key.only = dialects(dialectMySQL)
	return key
}

// The text forms of a time literal: MySQL's DATETIME form, and the form in
// which the SQLite driver writes a time.
const (
	mysqlTimeLayout  = "2006-01-02 15:04:05.999999"
	sqliteTimeLayout = "2006-01-02 15:04:05.999999999-07:00"
)

func localTimeKey[T any](
	column jet.ColumnTimestamp, nullable bool, value func(T) *time.Time, low, high time.Time,
) Key[T] {
	// A timestamp has no zone: its wall clock is the value. Read the wall
	// clock as if it were UTC, so the microseconds round trip cannot shift it.
	micros := microsCodec()
	codec := keyCodec[time.Time]{
		encode: func(v time.Time) any {
			return micros.encode(
				time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC),
			)
		},
		decode: micros.decode,
		accept: func(d dialect, v time.Time) error {
			switch d {
			case dialectMySQL:
				if v.Before(low) || v.After(high) {
					return errors.New("timestamp outside the column's range")
				}
			case dialectSQLite:
				if v.Before(sqliteMinTime) || v.After(sqliteMaxTime) {
					return errors.New("timestamp outside the four-digit years")
				}
			case dialectPostgres:
			}
			return nil
		},
	}
	return newKey(column, nullable, value, codec, literalBound(column, func(d dialect, v time.Time) ijet.Expression {
		switch d {
		case dialectMySQL:
			return ijet.String(v.Format(mysqlTimeLayout))
		case dialectSQLite:
			return ijet.String(v.Format(sqliteTimeLayout))
		case dialectPostgres:
		}
		return jet.TimestampT(v)
	}))
}
