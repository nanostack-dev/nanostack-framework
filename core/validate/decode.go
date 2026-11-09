package validate

import (
	"bytes"
	"encoding"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DecodeOptions returns the strict json/v2 options every request body and
// union variant is decoded with: duplicate member names and invalid UTF-8
// are rejected, and member names match case-sensitively.
//
// An integer accepts a JSON number whose value is an integer however it is
// written, such as 3.0 or 1e2: JSON Schema (draft 6 on, so OpenAPI 3.1) and
// kin-openapi define integer by value, not by spelling. 3.5 is still a type
// error and 1e30 is out of range. The options cover Go's integer types; a
// named integer type gets the same rule from the UnmarshalJSONFrom method
// nanostack gen validate writes for it (see [DecodeInteger]).
func DecodeOptions() jsonv2.Options {
	return jsonv2.JoinOptions(
		jsontext.AllowDuplicateNames(false),
		jsontext.AllowInvalidUTF8(false),
		jsonv2.MatchCaseInsensitiveNames(false),
		jsonv2.WithUnmarshalers(integerUnmarshalers),
	)
}

// integerUnmarshalers are typed, so json/v2 consults them only for these
// types and every other value decodes at full speed.
var integerUnmarshalers = jsonv2.JoinUnmarshalers(
	jsonv2.UnmarshalFromFunc(DecodeInteger[int]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[int8]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[int16]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[int32]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[int64]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[uint]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[uint8]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[uint16]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[uint32]),
	jsonv2.UnmarshalFromFunc(DecodeInteger[uint64]),
)

// Integer is any Go integer type.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// DecodeInteger decodes a JSON number whose value is an integer, such as 3,
// 3.0 or 1e2, into v. Anything but a number it leaves to json/v2's default
// decoding (it returns errors.ErrUnsupported without reading). A number with
// a fraction is a type error and a value v cannot hold a range error, as
// json/v2 reports them.
//
// nanostack gen validate writes it as the UnmarshalJSONFrom method of every
// named integer type in the package it validates, such as an integer enum:
//
//	func (v *Priority) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
//		return validate.DecodeInteger(dec, v)
//	}
func DecodeInteger[T Integer](dec *jsontext.Decoder, v *T) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	lit := []byte(val)
	if bytes.ContainsAny(val, ".eE") {
		s, integral := integralLiteral(string(val))
		if !integral {
			// The error json/v2 reports for a fraction: "must be an integer".
			return &jsonv2.SemanticError{JSONKind: '0', JSONValue: val.Clone(), GoType: reflect.TypeFor[T]()}
		}
		lit = []byte(s)
	}
	return setInteger(reflect.ValueOf(v).Elem(), lit)
}

// setInteger stores the integer literal lit, digits with an optional minus
// sign, in elem. Out of range, it reports the error json/v2 reports for the
// literal, which DecodeError describes as out of range.
func setInteger(elem reflect.Value, lit []byte) error {
	neg := len(lit) > 0 && lit[0] == '-'
	digits := lit
	if neg {
		digits = lit[1:]
	}
	var mag uint64
	inRange := true
	for _, c := range digits {
		d := uint64(c - '0')
		if mag > (math.MaxUint64-d)/10 {
			inRange = false
			break
		}
		mag = mag*10 + d
	}
	bits := elem.Type().Bits()
	if elem.CanInt() {
		limit := uint64(1) << (bits - 1)
		if inRange && (mag < limit || neg && mag == limit) {
			n := int64(mag) // mag <= 1<<63; -(1<<63) wraps to MinInt64 as intended
			if neg {
				n = -n
			}
			elem.SetInt(n)
			return nil
		}
	} else if inRange && (!neg || mag == 0) && (bits == 64 || mag < uint64(1)<<bits) {
		elem.SetUint(mag)
		return nil
	}
	return &jsonv2.SemanticError{JSONKind: '0', JSONValue: jsontext.Value(bytes.Clone(lit)), GoType: elem.Type(),
		Err: strconv.ErrRange}
}

// maxIntegerDigits is more digits than any Go integer holds; an integral
// literal longer than that is out of range whatever its exact value.
const maxIntegerDigits = 21

// integralLiteral rewrites the JSON number num, which the decoder has
// validated, as a plain integer literal when its value is an integer, such
// as "-1.50e1" as "-15". It never expands an exponent past maxIntegerDigits,
// so 1e999999999 costs nothing.
func integralLiteral(num string) (string, bool) {
	sign := ""
	if strings.HasPrefix(num, "-") {
		sign, num = "-", num[1:]
	}
	mant, exp := num, 0
	if i := strings.IndexAny(num, "eE"); i >= 0 {
		mant = num[:i]
		e, err := strconv.Atoi(num[i+1:])
		switch {
		case err == nil:
			exp = e
		case strings.HasPrefix(num[i+1:], "-"):
			exp = math.MinInt32 // tiny: an integer only if the mantissa is 0
		default:
			exp = math.MaxInt32
		}
	}
	whole, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimLeft(whole+frac, "0")
	if digits == "" {
		return "0", true
	}
	scale := exp - len(frac)
	for scale < 0 && strings.HasSuffix(digits, "0") {
		digits, scale = digits[:len(digits)-1], scale+1
	}
	if scale < 0 {
		return "", false
	}
	scale = min(scale, maxIntegerDigits)
	return sign + digits + strings.Repeat("0", scale), true
}

// DecodeError converts a json/v2 semantic error into a violation at p joined
// with the error's own JSON Pointer. It reports false for other errors. The
// message names the expected JSON type or format, never a Go type.
func DecodeError(p Path, err error) (Error, bool) {
	var se *jsonv2.SemanticError
	if !errors.As(err, &se) {
		return Error{}, false
	}
	code, msg := describeSemantic(se)
	return Error{Pointer: p.Join(string(se.JSONPointer)).ptr, Code: code, Detail: msg}, true
}

var textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()

func describeSemantic(se *jsonv2.SemanticError) (code, message string) {
	t := se.GoType
	if t == nil {
		return CodeType, "has an invalid value"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if f, ok := knownFormats[t.PkgPath()+"."+t.Name()]; ok {
		return CodeFormat, "must be " + f
	}
	got := jsonKindName(se.JSONKind)
	want := ""
	switch t.Kind() {
	case reflect.String:
		want = "string"
	case reflect.Bool:
		want = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if se.JSONKind == '0' {
			return integerRange(t, se.JSONValue)
		}
		want = "integer"
	case reflect.Float32, reflect.Float64:
		if se.JSONKind == '0' {
			return CodeFormat, "is out of range for " + t.Kind().String()
		}
		want = "number"
	case reflect.Slice, reflect.Array:
		want = "array"
		if t.Elem().Kind() == reflect.Uint8 {
			want = "string"
		}
	case reflect.Map, reflect.Struct:
		want = "object"
	default:
		// Interfaces, channels and the like: no JSON type to name.
	}
	if reflect.PointerTo(t).Implements(textUnmarshaler) && se.JSONKind == '"' {
		return CodeFormat, "has an invalid format"
	}
	if want == "" || got == "" {
		return CodeType, "has an invalid value"
	}
	if want == got {
		return CodeType, "has an invalid " + want + " value"
	}
	return CodeType, "must be " + article(want) + ", got " + got
}

// knownFormats maps the Go types oapi-codegen uses for string formats to the
// format a client sees in the contract.
var knownFormats = map[string]string{
	reflect.TypeFor[time.Time]().PkgPath() + ".Time":         "an RFC 3339 date-time",
	"github.com/google/uuid.UUID":                            "a UUID",
	"github.com/oapi-codegen/runtime/types.UUID":             "a UUID",
	"github.com/oapi-codegen/runtime/types.Date":             "an RFC 3339 full-date (YYYY-MM-DD)",
	"github.com/oapi-codegen/runtime/types.Email":            "an email address",
	reflect.TypeFor[time.Duration]().PkgPath() + ".Duration": "a duration",
}

// integerRange explains a JSON number an integer field rejected: a
// fraction or exponent is the wrong type, digits only are out of range.
func integerRange(t reflect.Type, v jsontext.Value) (code, message string) {
	if !digits(strings.TrimPrefix(string(v), "-")) {
		return CodeType, "must be an integer"
	}
	return CodeFormat, "is out of range for " + t.Kind().String()
}

func jsonKindName(k jsontext.Kind) string {
	switch k {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '0':
		return "number"
	case '{':
		return "object"
	case '[':
		return "array"
	default:
		return ""
	}
}

func article(s string) string {
	switch s[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an " + s
	}
	return "a " + s
}

// Variant decodes the JSON text raw, a union value at p, as a T with the
// strict options. Decode failures come back as violations under p. Generated
// validators use it to check one branch of a oneOf or anyOf.
func Variant[T any](raw []byte, p Path) (T, Errors) {
	var v T
	if err := jsonv2.Unmarshal(raw, &v, DecodeOptions()); err != nil {
		if e, ok := DecodeError(p, err); ok {
			return v, Errors{e}
		}
		return v, Errors{{Pointer: p.ptr, Code: CodeType, Detail: "is not valid JSON"}}
	}
	return v, nil
}

// Discriminator reads the string member name of the object raw at p, the
// discriminator of a oneOf or anyOf. On failure it records required or type
// at p.Field(name) and reports false.
func (e *Errors) Discriminator(raw []byte, p Path, name string) (string, bool) {
	var obj map[string]jsontext.Value
	if err := jsonv2.Unmarshal(raw, &obj, DecodeOptions()); err != nil {
		e.Add(p, CodeType, "must be an object")
		return "", false
	}
	v, found := obj[name]
	if !found {
		e.Add(p.Field(name), CodeRequired, "is required")
		return "", false
	}
	var s string
	if v.Kind() != '"' || jsonv2.Unmarshal(v, &s) != nil {
		e.Add(p.Field(name), CodeType, "must be a string")
		return "", false
	}
	return s, true
}

// JSONType reports whether v, a value decoded into an interface, has one of
// the JSON Schema types in types ("null", "boolean", "object", "array",
// "number", "string" or "integer").
func JSONType(v any, types ...string) bool {
	kind := jsonTypeOf(v)
	for _, t := range types {
		if t == kind || t == "number" && kind == "integer" {
			return true
		}
	}
	return false
}

// jsonTypeOf names the JSON Schema type of a decoded value; an integral
// number is "integer".
func jsonTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return "integer"
		}
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return ""
}
