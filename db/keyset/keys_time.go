package keyset

import (
	"encoding/json"
	"errors"
	"time"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// TimeKey orders on a NOT NULL timestamptz column.
func TimeKey[T any](column jet.ColumnTimestampz, value func(T) time.Time) Key[T] {
	return timeKey(column, false, ptrOf(value))
}

// NullableTimeKey orders on a nullable timestamptz column.
func NullableTimeKey[T any](column jet.ColumnTimestampz, value func(T) *time.Time) Key[T] {
	return timeKey(column, true, value)
}

// PostgreSQL stores timestamps from 4714-11-24 BC to 294276-12-31 AD at
// microsecond precision.
var (
	postgresMinTime = time.Date(-4713, time.November, 24, 0, 0, 0, 0, time.UTC)
	postgresMaxTime = time.Date(294276, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)
	// postgresEpochUnix is 2000-01-01 UTC, the PostgreSQL timestamp epoch.
	postgresEpochUnix = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
)

// microsCodec encodes an instant as microseconds since 2000-01-01 UTC, the
// form PostgreSQL itself stores. It is exact across the whole PostgreSQL
// range. Unix microseconds overflow past year 294247, RFC 3339 loses the
// seconds of an offset and cannot write years past 9999, and a decoded value
// carries no zone that the database could refuse. The arithmetic goes through
// seconds, because a time.Duration overflows after 292 years.
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
	return newCodecKey(column, nullable, value, microsCodec(),
		func(v time.Time) jet.Expression { return jet.TimestampzT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampzT(v)), column.LT(jet.TimestampzT(v)), column.EQ(jet.TimestampzT(v))
		})
}

// LocalTimeKey orders on a NOT NULL timestamp (without time zone) column.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return localTimeKey(column, false, ptrOf(value))
}

// NullableLocalTimeKey orders on a nullable timestamp (without time zone) column.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return localTimeKey(column, true, value)
}

func localTimeKey[T any](column jet.ColumnTimestamp, nullable bool, value func(T) *time.Time) Key[T] {
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
	}
	return newCodecKey(column, nullable, value, codec,
		func(v time.Time) jet.Expression { return jet.TimestampT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampT(v)), column.LT(jet.TimestampT(v)), column.EQ(jet.TimestampT(v))
		})
}
