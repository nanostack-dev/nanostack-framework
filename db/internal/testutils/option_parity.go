// This file is Nanostack-only (not in go-jet): test helpers for functional.Option.
// See db/UPSTREAM.md.

package testutils

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/internal/optional"
)

// OptionComparable lets go-cmp (AssertDeepEqual) compare functional.Option values, whose
// fields are unexported. Equal methods of the carried values (time.Time,
// decimal.Decimal) still apply.
var OptionComparable = cmp.Exporter(func(t reflect.Type) bool {
	_, ok := optional.Elem(t)
	return ok
})

// AssertOptionParity checks that optRow, a struct with functional.Option fields, holds
// the same data as ptrRow, the same struct generated with pointers. It walks optRow's
// fields by name. An Option field is compared through ToPtr with the pointer field;
// other fields directly. Named types with the same underlying type (an enum generated
// into two packages) are converted before the comparison.
func AssertOptionParity(t require.TestingT, optRow, ptrRow any) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	opt := reflect.Indirect(reflect.ValueOf(optRow))
	ptr := reflect.Indirect(reflect.ValueOf(ptrRow))
	require.Equal(t, reflect.Struct, opt.Kind())
	require.Equal(t, reflect.Struct, ptr.Kind())

	for i := 0; i < opt.NumField(); i++ {
		name := opt.Type().Field(i).Name
		optField := opt.Field(i)
		ptrField := ptr.FieldByName(name)
		require.True(t, ptrField.IsValid(), "field %s is missing in %s", name, ptr.Type())

		got := optField
		if _, isOpt := optional.Elem(optField.Type()); isOpt {
			require.Equal(
				t,
				reflect.Pointer,
				ptrField.Kind(),
				"field %s: Option field against a non-pointer %s",
				name,
				ptrField.Type(),
			)
			got = optField.MethodByName("ToPtr").Call(nil)[0]
		}
		AssertDeepEqual(t, convertLike(t, name, got, ptrField.Type()).Interface(), ptrField.Interface())
	}
}

// convertLike converts v to type to when their types differ only by name.
func convertLike(t require.TestingT, field string, v reflect.Value, to reflect.Type) reflect.Value {
	if v.Type() == to {
		return v
	}
	if v.Kind() == reflect.Pointer && to.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Zero(to)
		}
		out := reflect.New(to.Elem())
		out.Elem().Set(convertLike(t, field, v.Elem(), to.Elem()))
		return out
	}
	require.True(t, v.Type().ConvertibleTo(to), "field %s: %s is not convertible to %s", field, v.Type(), to)
	return v.Convert(to)
}

// AssertOptLiteral checks an XxxOpt literal constructor against its plain constructor:
// some (XxxOpt(Some(v))) must serialize exactly like plain (Xxx(v)); none (XxxOpt(None))
// must give the same SQL with a nil argument, and debugNone in debug SQL.
func AssertOptLiteral(t *testing.T, dialect jet.Dialect, plain, some, none jet.Serializer, debugNone string) {
	t.Helper()
	serialize := func(s jet.Serializer, debug bool) (string, []any) {
		out := jet.SQLBuilder{Dialect: dialect, Debug: debug}
		jet.Serialize(s, jet.SelectStatementType, &out)
		return out.Buff.String(), out.Args
	}

	plainSQL, plainArgs := serialize(plain, false)
	someSQL, someArgs := serialize(some, false)
	require.Equal(t, plainSQL, someSQL, "Some must serialize like the plain constructor")
	require.Equal(t, plainArgs, someArgs, "Some must bind the same argument as the plain constructor")

	plainDebug, _ := serialize(plain, true)
	someDebug, _ := serialize(some, true)
	require.Equal(t, plainDebug, someDebug)

	noneSQL, noneArgs := serialize(none, false)
	require.Equal(t, plainSQL, noneSQL, "None must keep the SQL (and cast) of the plain constructor")
	require.Equal(t, make([]any, len(plainArgs)), noneArgs, "None must bind nil, got %v", noneArgs)

	noneDebug, _ := serialize(none, true)
	require.Equal(t, debugNone, noneDebug)
}

// AssertOptionFields checks that every functional.Option field of row is present (want
// true) or absent (want false), and returns how many Option fields it checked.
func AssertOptionFields(t require.TestingT, row any, want bool) int {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	v := reflect.Indirect(reflect.ValueOf(row))
	checked := 0
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if _, isOpt := optional.Elem(field.Type()); !isOpt {
			continue
		}
		_, present := optional.Get(field)
		require.Equal(t, want, present, "field %s.%s", v.Type(), v.Type().Field(i).Name)
		checked++
	}
	return checked
}

// AssertPointerFieldsNil checks that every pointer field of row is nil.
func AssertPointerFieldsNil(t require.TestingT, row any) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	v := reflect.Indirect(reflect.ValueOf(row))
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Pointer {
			require.True(t, v.Field(i).IsNil(), "field %s.%s", v.Type(), v.Type().Field(i).Name)
		}
	}
}
