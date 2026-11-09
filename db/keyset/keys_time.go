package keyset

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// MariaDB 11.5 and later, on a 64-bit build, holds TIMESTAMP values up to
	// 2106-02-07 06:28:15 UTC.
	mariadbTimestampMaxTime = time.Date(2106, time.February, 7, 6, 28, 15, 999_999_000, time.UTC)
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
// With fine set, an instant with a finer fraction than a microsecond is the
// pair [microseconds, nanoseconds past them], with the nanoseconds from 1 to
// 999. A SQLite column can store such an instant (the driver writes nine
// fraction digits), and a cursor cut to microseconds would sort before the
// row and repeat it. Every other instant keeps the single number, so its
// token does not change. Without fine, the codec cuts the fraction to
// microseconds: a PostgreSQL timestamptz holds no finer value.
//
// decode refuses an instant outside the PostgreSQL range, and a pair that is
// not in its one canonical form. LocalTimeKey refuses a pair on PostgreSQL and
// MySQL, whose columns hold microseconds.
func microsCodec(fine bool) keyCodec[time.Time] {
	return keyCodec[time.Time]{
		encode: func(v time.Time) any {
			micros := (v.Unix()-postgresEpochUnix)*1_000_000 + int64(v.Nanosecond()/1_000)
			if nanos := int64(v.Nanosecond() % 1_000); fine && nanos != 0 {
				return []int64{micros, nanos}
			}
			return micros
		},
		decode: func(raw json.RawMessage) (time.Time, error) {
			var micros, nanos int64
			if err := json.Unmarshal(raw, &micros); err != nil {
				var pair []int64
				if !fine || json.Unmarshal(raw, &pair) != nil || len(pair) != 2 || pair[1] < 1 || pair[1] > 999 {
					return time.Time{}, err
				}
				micros, nanos = pair[0], pair[1]
			}
			seconds, remainder := micros/1_000_000, micros%1_000_000
			if remainder < 0 {
				seconds, remainder = seconds-1, remainder+1_000_000
			}
			decoded := time.Unix(postgresEpochUnix+seconds, remainder*1_000+nanos).UTC()
			if decoded.Before(postgresMinTime) || decoded.After(postgresMaxTime) {
				return time.Time{}, errors.New("timestamp outside the PostgreSQL range")
			}
			return decoded, nil
		},
	}
}

// acceptMicros refuses an instant with a finer fraction than a microsecond,
// which a PostgreSQL or MySQL column cannot hold.
func acceptMicros(v time.Time) error {
	if v.Nanosecond()%1_000 != 0 {
		return errors.New("timestamp finer than a microsecond")
	}
	return nil
}

func timeKey[T any](column jet.ColumnTimestampz, nullable bool, value func(T) *time.Time) Key[T] {
	key := newKey(
		column,
		nullable,
		value,
		microsCodec(false),
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
// text, so the driver's loc setting cannot shift it. The zero date
// '0000-00-00 00:00:00', which MySQL stores when sql_mode allows it, reads as
// time.Time{} with parseTime=true. It sorts before every date, and it is a
// valid cursor value. A MySQL TIMESTAMP column takes MySQLTimestampKey.
//
// SQLite has no time type: the column holds text and compares as text. By
// default the literal is the cursor's time in the form in which the driver
// writes a time.Time, "2006-01-02 15:04:05.999999999-07:00", with a +00:00
// offset. So every row must be written through the driver, as a time.Time in
// UTC. A value that SQL writes (CURRENT_TIMESTAMP, datetime('now'),
// strftime, a text literal) or another client writes has another text form:
// a walk then skips the rows that tie with the cursor row, or repeats them
// without end. A column that SQL fills takes SQLiteLayout with its text form.
// No layout fits a column that holds both forms. The text sorts in time order
// for four-digit years only, so a cursor value outside the years 0000 to 9999
// is search.ErrInvalidCursor.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return localTimeKey(column, false, ptrOf(value), datetimeMinTime, datetimeMaxTime)
}

// NullableLocalTimeKey orders on a nullable timestamp without time zone.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return localTimeKey(column, true, value, datetimeMinTime, datetimeMaxTime)
}

// MySQLTimestampKey orders on a NOT NULL MySQL TIMESTAMP column, or a MariaDB
// TIMESTAMP before MariaDB 11.5. A cursor value outside the TIMESTAMP range
// (1970-01-01 00:00:01 to 2038-01-19 03:14:07) is search.ErrInvalidCursor,
// except the zero date, as in LocalTimeKey. MySQL reads and compares a
// TIMESTAMP in the session time zone, and the range is checked in UTC, so run
// the session in UTC: in a zone with daylight saving time, the repeated hour
// maps two instants to one wall clock, and a walk through it can skip or
// repeat rows. PostgreSQL and SQLite have no such type: Page returns
// ErrDialect.
func MySQLTimestampKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return mysqlTimestampKey(column, false, ptrOf(value), timestampMaxTime)
}

// NullableMySQLTimestampKey orders on a nullable MySQL TIMESTAMP column.
func NullableMySQLTimestampKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return mysqlTimestampKey(column, true, value, timestampMaxTime)
}

// MariaDBTimestampKey orders on a NOT NULL TIMESTAMP column of MariaDB 11.5 or
// later on a 64-bit build, which holds 1970-01-01 00:00:01 to 2106-02-07
// 06:28:15 UTC. It is MySQLTimestampKey with that range.
func MariaDBTimestampKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return mysqlTimestampKey(column, false, ptrOf(value), mariadbTimestampMaxTime)
}

// NullableMariaDBTimestampKey orders on a nullable MariaDB 11.5 TIMESTAMP
// column.
func NullableMariaDBTimestampKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return mysqlTimestampKey(column, true, value, mariadbTimestampMaxTime)
}

func mysqlTimestampKey[T any](
	column jet.ColumnTimestamp, nullable bool, value func(T) *time.Time, high time.Time,
) Key[T] {
	key := localTimeKey(column, nullable, value, timestampMinTime, high)
	key.only = dialects(dialectMySQL)
	return key
}

// SQLiteLayout returns the key with the SQLite literal written in layout, the
// Go time layout of the column's stored text. Use "2006-01-02 15:04:05" for a
// column filled by CURRENT_TIMESTAMP or datetime('now'). Every row of the
// column must hold exactly that form. A cursor value that the layout cannot
// write exactly (a fraction that it drops) is search.ErrInvalidCursor on
// SQLite. The layout must start with "2006-01-02 15:04:05" or
// "2006-01-02T15:04:05", so that the text sorts in time order.
//
// It applies to LocalTimeKey and NullableLocalTimeKey, and panics on any
// other key. The layout comes from code, never from a request.
func (k Key[T]) SQLiteLayout(layout string) Key[T] {
	if k.withSQLiteLayout == nil {
		panic(fmt.Sprintf("keyset: SQLiteLayout on column %s, which is not a LocalTimeKey", k.column.Name()))
	}
	if !strings.HasPrefix(layout, "2006-01-02 15:04:05") && !strings.HasPrefix(layout, "2006-01-02T15:04:05") {
		panic(fmt.Sprintf("keyset: SQLite layout %q does not sort in time order", layout))
	}
	k.bound = k.withSQLiteLayout(layout)
	return k
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
	key := newKey(column, nullable, value, localTimeCodec(low, high, sqliteTimeLayout),
		localTimeBound(column, low, sqliteTimeLayout))
	key.withSQLiteLayout = func(layout string) func(dialect, json.RawMessage) (keyBound, error) {
		relaid := newKey(column, nullable, value, localTimeCodec(low, high, layout),
			localTimeBound(column, low, layout))
		return relaid.bound
	}
	return key
}

// localTimeCodec reads a timestamp without time zone. The MySQL column holds
// [low, high] and the zero date. The SQLite column holds the four-digit years
// that layout writes exactly.
func localTimeCodec(low, high time.Time, layout string) keyCodec[time.Time] {
	// A timestamp has no zone: its wall clock is the value. Read the wall
	// clock as if it were UTC, so the microseconds round trip cannot shift it.
	micros := microsCodec(true)
	return keyCodec[time.Time]{
		encode: func(v time.Time) any {
			return micros.encode(
				time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC),
			)
		},
		decode: micros.decode,
		accept: func(d dialect, v time.Time) error {
			switch d {
			case dialectMySQL:
				if v.Equal(time.Time{}) {
					return nil
				}
				if v.Before(low) || v.After(high) {
					return errors.New("timestamp outside the column's range")
				}
				return acceptMicros(v)
			case dialectSQLite:
				if v.Before(sqliteMinTime) || v.After(sqliteMaxTime) {
					return errors.New("timestamp outside the four-digit years")
				}
				if written, err := time.Parse(layout, v.Format(layout)); err != nil || !written.Equal(v) {
					return errors.New("timestamp that the column's text form cannot write")
				}
			case dialectPostgres:
				return acceptMicros(v)
			}
			return nil
		},
	}
}

// localTimeBound compares the column with the cursor's wall clock. On MySQL
// the zero date sorts before low, the column's first valid date. The bound
// after the zero date names low, not a zero-date literal, so it does not
// depend on what the session's sql_mode (NO_ZERO_DATE) does with such a
// literal.
func localTimeBound(column jet.ColumnTimestamp, low time.Time, layout string) boundFunc[time.Time] {
	return func(d dialect, v time.Time) keyBound {
		switch d {
		case dialectMySQL:
			if v.Equal(time.Time{}) {
				first := ijet.String(low.Format(mysqlTimeLayout))
				return keyBound{literal: first, gt: ijet.GtEq(column, first), eq: ijet.Lt(column, first)}
			}
			return columnBound(column, ijet.String(v.Format(mysqlTimeLayout)))
		case dialectSQLite:
			return columnBound(column, ijet.String(v.Format(layout)))
		case dialectPostgres:
		}
		return columnBound(column, jet.TimestampT(v))
	}
}
