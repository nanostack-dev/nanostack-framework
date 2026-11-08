package validate

import (
	jsonv2 "encoding/json/v2"
	"math/big"
	"reflect"
	"strconv"
)

// Unique reports the first pair of equal elements of s, for uniqueItems on
// arrays of comparable values without pointers.
func Unique[T comparable](s []T) (first, second int, unique bool) {
	seen := make(map[T]int, len(s))
	for i, v := range s {
		if j, dup := seen[v]; dup {
			return j, i, false
		}
		seen[v] = i
	}
	return 0, 0, true
}

// UniqueJSON is Unique for any element type: two elements are equal when
// their deterministic JSON encodings are equal, which is JSON Schema
// equality for decoded values.
func UniqueJSON[T any](s []T) (first, second int, unique bool) {
	seen := make(map[string]int, len(s))
	for i, v := range s {
		b, err := jsonv2.Marshal(v, jsonv2.Deterministic(true))
		if err != nil {
			continue
		}
		if j, dup := seen[string(b)]; dup {
			return j, i, false
		}
		seen[string(b)] = i
	}
	return 0, 0, true
}

// DuplicateMessage is the message of a uniqueItems violation.
func DuplicateMessage(first, second int) string {
	return "must not contain duplicate items (" + strconv.Itoa(first) + " and " + strconv.Itoa(second) + " are equal)"
}

// Number is any Go type a JSON Schema number or integer decodes into.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// MultipleOf reports whether v is an integer multiple of m, which is the
// decimal literal of the schema's multipleOf. It divides exactly, using the
// shortest decimal that round-trips v at its own precision, so 0.3 is a
// multiple of 0.1 for float32 and float64 alike.
func MultipleOf[T Number](v T, m string) bool {
	mr, ok := new(big.Rat).SetString(m)
	if !ok || mr.Sign() == 0 {
		return false
	}
	var lit string
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Float32:
		lit = strconv.FormatFloat(rv.Float(), 'g', -1, 32)
	case reflect.Float64:
		lit = strconv.FormatFloat(rv.Float(), 'g', -1, 64)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		lit = strconv.FormatInt(rv.Int(), 10)
	default:
		lit = strconv.FormatUint(rv.Uint(), 10)
	}
	vr, ok := new(big.Rat).SetString(lit)
	if !ok {
		return false
	}
	return new(big.Rat).Quo(vr, mr).IsInt()
}
