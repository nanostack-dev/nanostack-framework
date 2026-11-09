// Package configtest checks a hand-written default Config against the
// default: struct tags that nanostack gen config reads, so the tags stay the one
// source of truth for the defaults.
package configtest

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

// AssertDefaults fails t when a field of cfg, a struct, differs from its
// default: tag, or when a field without the tag is not zero. It handles the
// tag kinds the framework's module Configs use: string, bool, integer,
// float and time.Duration.
func AssertDefaults(t testing.TB, cfg any) {
	t.Helper()
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Struct {
		t.Fatalf("configtest: %T is not a struct", cfg)
	}
	for i := range v.NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		got := v.Field(i)
		tag, ok := field.Tag.Lookup("default")
		if !ok {
			if !got.IsZero() {
				t.Errorf("%s.%s = %v, want zero: it has no default tag", v.Type(), field.Name, got)
			}
			continue
		}
		want, err := parse(field.Type, tag)
		if err != nil {
			t.Errorf("%s.%s: default %q: %v", v.Type(), field.Name, tag, err)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), want.Interface()) {
			t.Errorf("%s.%s = %v, want the default tag %q", v.Type(), field.Name, got, tag)
		}
	}
}

func parse(typ reflect.Type, s string) (reflect.Value, error) {
	out := reflect.New(typ).Elem()
	if typ == reflect.TypeFor[time.Duration]() {
		d, err := time.ParseDuration(s)
		out.SetInt(int64(d))
		return out, err
	}
	switch typ.Kind() {
	case reflect.String:
		out.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		out.SetBool(b)
		return out, err
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, typ.Bits())
		out.SetInt(n)
		return out, err
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, typ.Bits())
		out.SetUint(n)
		return out, err
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, typ.Bits())
		out.SetFloat(f)
		return out, err
	default:
		return out, &unsupportedError{typ: typ}
	}
	return out, nil
}

type unsupportedError struct{ typ reflect.Type }

func (e *unsupportedError) Error() string {
	return "configtest: unsupported field type " + e.typ.String()
}
