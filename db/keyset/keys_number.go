package keyset

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

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

func intKey[T any](column jet.ColumnInteger, nullable bool, value func(T) *int64) Key[T] {
	// The literal is cast to bigint: a bare parameter would take the column's
	// type, and a forged value past smallint or integer would fail to bind.
	// PostgreSQL compares integer widths across one btree family, so an index
	// on the column still serves the comparison.
	typed := func(v int64) jet.IntegerExpression { return jet.IntExp(jet.CAST(jet.Int(v)).AS_BIGINT()) }
	return newKey(column, nullable, value,
		func(v int64) jet.Expression { return typed(v) },
		func(v int64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
}

// FloatKey orders on a NOT NULL double precision column. It does not fit
// numeric, where float64 rounding would repeat a row forever, nor real, which
// takes RealKey. NaN and the infinities, which PostgreSQL orders above every
// number, round-trip exactly.
func FloatKey[T any](column jet.ColumnFloat, value func(T) float64) Key[T] {
	return floatKey(column, false, ptrOf(value))
}

// NullableFloatKey orders on a nullable double precision column.
func NullableFloatKey[T any](column jet.ColumnFloat, value func(T) *float64) Key[T] {
	return floatKey(column, true, value)
}

// typedFloat casts the literal to double precision explicitly.
func typedFloat(v float64) jet.FloatExpression {
	return jet.FloatExp(jet.CAST(jet.Float(v)).AS_DOUBLE())
}

// RealKey orders on a NOT NULL real column. A real compared with a double
// literal is promoted, and 0.1 stored as real is not 0.1 as double, so the
// boundary row would match itself and repeat. The key compares as real. It
// refuses a finite cursor value outside real's range with ErrInvalidCursor:
// the conversion to float32 would turn it into an infinity, and the page
// would jump to the end.
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
	}
	typed := func(v float32) jet.FloatExpression {
		return jet.FloatExp(jet.CAST(jet.Float(float64(v))).AS_REAL())
	}
	return newCodecKey(column, nullable, value, codec,
		func(v float32) jet.Expression { return typed(v) },
		func(v float32) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
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
	}
}

func floatKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float64) Key[T] {
	return newCodecKey(column, nullable, value, floatCodec(),
		func(v float64) jet.Expression { return typedFloat(v) },
		func(v float64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typedFloat(v)), column.LT(typedFloat(v)), column.EQ(typedFloat(v))
		})
}
