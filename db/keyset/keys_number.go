package keyset

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// IntKey orders on a NOT NULL integer column.
func IntKey[T any](column jet.ColumnInteger, value func(T) int64) Key[T] {
	return intKey(column, false, ptrOf(value))
}

// NullableIntKey orders on a nullable integer column.
func NullableIntKey[T any](column jet.ColumnInteger, value func(T) *int64) Key[T] {
	return intKey(column, true, value)
}

// intLiteral casts the literal to a 64-bit integer where a bare parameter
// could take a narrower type. On PostgreSQL a bare parameter takes the
// column's type, so a forged value past smallint or integer would fail to
// bind. PostgreSQL compares integer widths across one btree family, so an
// index on the column still serves the comparison. MySQL gets CAST(? AS
// SIGNED). SQLite integers are 64-bit, and a cast there would only hide the
// column's affinity.
func intLiteral(d dialect, v int64) ijet.Expression {
	switch d {
	case dialectPostgres:
		return jet.CAST(jet.Int(v)).AS_BIGINT()
	case dialectMySQL:
		return mysql.CAST(mysql.Int(v)).AS_SIGNED()
	case dialectSQLite:
	}
	return ijet.Int(v)
}

func intKey[T any](column jet.ColumnInteger, nullable bool, value func(T) *int64) Key[T] {
	return newKey(column, nullable, value, jsonCodec[int64](), literalBound(column, intLiteral))
}

// FloatKey orders on a NOT NULL double precision column. It does not fit
// numeric or decimal, where float64 rounding would repeat a row forever, nor
// real (MySQL FLOAT), which takes RealKey.
//
// NaN and the infinities round-trip exactly on PostgreSQL, which orders them
// above every number. MySQL's DOUBLE holds none of them, and SQLite stores NaN
// as NULL, so a cursor that carries one is refused there with
// search.ErrInvalidCursor.
func FloatKey[T any](column jet.ColumnFloat, value func(T) float64) Key[T] {
	return floatKey(column, false, ptrOf(value))
}

// NullableFloatKey orders on a nullable double precision column.
func NullableFloatKey[T any](column jet.ColumnFloat, value func(T) *float64) Key[T] {
	return floatKey(column, true, value)
}

// floatLiteral casts the literal to double precision on PostgreSQL. MySQL and
// SQLite bind a float64 as a double.
func floatLiteral(d dialect, v float64) ijet.Expression {
	if d == dialectPostgres {
		return jet.CAST(jet.Float(v)).AS_DOUBLE()
	}
	return ijet.Float(v)
}

// acceptFloat refuses the values a dialect's floating-point column cannot
// hold: NaN and the infinities on MySQL, NaN on SQLite.
func acceptFloat(d dialect, v float64) error {
	switch d {
	case dialectMySQL:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("MySQL holds no NaN or infinity")
		}
	case dialectSQLite:
		if math.IsNaN(v) {
			return errors.New("SQLite holds no NaN")
		}
	case dialectPostgres:
	}
	return nil
}

// RealKey orders on a NOT NULL real column (MySQL FLOAT). A real compared with
// a double literal is promoted, and 0.1 stored as real is not 0.1 as double,
// so the boundary row would match itself and repeat. The key compares as real:
// with a cast on PostgreSQL, and with the float64 of the float32 value on MySQL
// and SQLite, which equals the promoted column value. It refuses a finite
// cursor value outside real's range with ErrInvalidCursor: the conversion to
// float32 would turn it into an infinity, and the page would jump to the end.
// NaN and the infinities follow FloatKey's rules per dialect.
func RealKey[T any](column jet.ColumnFloat, value func(T) float32) Key[T] {
	return realKey(column, false, ptrOf(value))
}

// NullableRealKey orders on a nullable real column.
func NullableRealKey[T any](column jet.ColumnFloat, value func(T) *float32) Key[T] {
	return realKey(column, true, value)
}

func realKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float32) Key[T] {
	double := floatCodec()
	codec := keyCodec[float32]{
		encode: func(v float32) any { return double.encode(float64(v)) },
		decode: func(raw json.RawMessage) (float32, error) {
			v, err := double.decode(raw)
			if err != nil {
				return 0, err
			}
			if !math.IsInf(v, 0) && !math.IsNaN(v) && math.Abs(v) > math.MaxFloat32 {
				return 0, errors.New("outside the real range")
			}
			return float32(v), nil
		},
		accept: func(d dialect, v float32) error { return acceptFloat(d, float64(v)) },
	}
	return newKey(column, nullable, value, codec, literalBound(column, func(d dialect, v float32) ijet.Expression {
		if d == dialectPostgres {
			return jet.CAST(jet.Float(float64(v))).AS_REAL()
		}
		return ijet.Float(float64(v))
	}))
}

// floatCodec writes NaN and the infinities as words, which JSON numbers
// cannot express.
func floatCodec() keyCodec[float64] {
	return keyCodec[float64]{
		encode: func(v float64) any {
			switch {
			case math.IsNaN(v):
				return "NaN"
			case math.IsInf(v, 1):
				return "Infinity"
			case math.IsInf(v, -1):
				return "-Infinity"
			default:
				return v
			}
		},
		decode: func(raw json.RawMessage) (float64, error) {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				switch text {
				case "NaN":
					return math.NaN(), nil
				case "Infinity":
					return math.Inf(1), nil
				case "-Infinity":
					return math.Inf(-1), nil
				default:
					return 0, fmt.Errorf("unknown float %q", text)
				}
			}
			var v float64
			err := json.Unmarshal(raw, &v)
			return v, err
		},
		accept: acceptFloat,
	}
}

func floatKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float64) Key[T] {
	return newKey(column, nullable, value, floatCodec(), literalBound(column, floatLiteral))
}
