// This file is Nanostack-only (not in go-jet): functional.Option arguments.
// See db/UPSTREAM.md.

package jet

import (
	"fmt"
	"reflect"

	"github.com/nanostack-dev/nanostack-framework/db/internal/optional"
)

// unwrapOptionArg returns the value a functional.Option argument carries, nil when it
// is absent (written as NULL), and any other argument unchanged. Every argument passes
// through SQLBuilder, so this covers MODEL, MODELS, VALUES, SET, Literal, FixedLiteral,
// Raw named arguments and DebugSql in every dialect.
//
// A *functional.Option argument panics, as a *functional.Option destination field does
// in qrm: the Option already says whether there is a value, and a pointer would add a
// second, ambiguous absence.
func unwrapOptionArg(arg any) any {
	if t := reflect.TypeOf(arg); t != nil && t.Kind() == reflect.Pointer && optional.IsOption(t.Elem()) {
		panic(
			fmt.Sprintf(
				"jet: a pointer to functional.Option is not supported as an SQL argument (%s); use %s",
				t,
				t.Elem(),
			),
		)
	}
	v, _ := optional.Unwrap(arg)
	return v
}

// rejectOptionPointerField panics when a MODEL field is a *functional.Option. MODEL
// dereferences pointer fields before the value reaches unwrapOptionArg, so the check
// happens here, with the column name in the message.
func rejectOptionPointerField(field reflect.Value, column string) {
	if t := field.Type(); t.Kind() == reflect.Pointer && optional.IsOption(t.Elem()) {
		panic(
			fmt.Sprintf(
				"jet: a pointer to functional.Option is not supported (column %s, field type %s); use %s",
				column,
				t,
				t.Elem(),
			),
		)
	}
}
