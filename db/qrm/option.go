// This file is Nanostack-only (not in go-jet): functional.Option[T] destinations.
// See db/UPSTREAM.md.

package qrm

import (
	"fmt"
	"reflect"

	"github.com/nanostack-dev/nanostack-framework/db/internal/optional"
)

// The field mapping kinds of a functional.Option[E] field whose E is a column value,
// outside the upstream iota range. classifyOption decides once per field whether E is
// a sql.Scanner, so assignOption does no per-value type check.
const (
	optionalType        fieldMappingType = 100 // E is a simple model type
	optionalScannerType fieldMappingType = 101 // E or *E implements sql.Scanner
)

// classifyOption returns the mapping kind of an Option[E] (or *Option[E]) field.
// Unsupported shapes panic, like other unsupported destinations: they are programming errors that the
// first test run catches.
func classifyOption(elem reflect.Type, field reflect.StructField) fieldMappingType {
	switch {
	case field.Type.Kind() == reflect.Pointer:
		panic(fmt.Sprintf("jet: a pointer to functional.Option is not supported (field %s %s); use %s",
			field.Name, field.Type, field.Type.Elem()))
	case elem.Kind() == reflect.Pointer:
		panic(
			fmt.Sprintf(
				"jet: functional.Option of a pointer is not supported (field %s %s); use functional.Option[%s] or %s",
				field.Name,
				field.Type,
				elem.Elem(),
				elem,
			),
		)
	case isOption(elem):
		panic(fmt.Sprintf("jet: nested functional.Option is not supported (field %s %s)", field.Name, field.Type))
	case implementsScannerType(elem):
		return optionalScannerType
	case isSimpleModelType(elem):
		return optionalType
	case elem.Kind() == reflect.Struct:
		return complexType
	default:
		panic(
			fmt.Sprintf(
				"jet: unsupported functional.Option element %s (field %s %s); use %s, or a json_column tag for JSON",
				elem,
				field.Name,
				field.Type,
				elem,
			),
		)
	}
}

func isOption(t reflect.Type) bool {
	_, ok := optional.Elem(t)
	return ok
}

// assignOption converts a non-NULL scanned value into a fresh E with qrm's existing
// rules, then stores it with optional.Set. It never calls Option.Scan with a raw driver
// value, so an Option[E] field reads exactly what a *E field reads. kind is the field's
// mapping kind, optionalType or optionalScannerType.
func assignOption(scanned reflect.Value, field reflect.Value, kind fieldMappingType) error {
	elem, _ := optional.Elem(field.Type())
	tmp := reflect.New(elem).Elem()

	var err error
	if kind == optionalScannerType {
		err = getScanner(tmp).Scan(scanned.Interface())
	} else {
		err = assign(scanned, tmp) // tryAssign + tryConvert, exactly as for a *E field
	}
	if err != nil {
		return err
	}
	return optional.Set(field, tmp)
}

// mapRowToOption maps a nested Option[Struct]. It mirrors a nil *Struct field: the
// struct is created, mapped, and stored only when something was mapped. When the Option
// is already present (a later row of the same parent), mapping starts from its current
// value, so nested slices keep growing across rows, like they do behind a non-nil *Struct.
func mapRowToOption(
	scanContext *ScanContext,
	groupKey string,
	dest reflect.Value,
	elem reflect.Type,
	field *reflect.StructField) (updated bool, err error) {

	tmp := reflect.New(elem)
	if cur, present := optional.Get(dest); present {
		tmp.Elem().Set(cur)
	}

	updated, err = mapRowToDestinationPtr(scanContext, groupKey, tmp, field)
	if err != nil || !updated {
		return updated, err
	}

	return true, optional.Set(dest, tmp.Elem())
}

// rejectOptionRows panics when rows would be mapped into functional.Option values: a
// []Option[T] destination, or a top-level Option destination.
func rejectOptionRows(elemType reflect.Type, field *reflect.StructField) {
	if isOption(elemType) {
		panic("jet: functional.Option[T] is not a supported row destination" + fieldToString(field) +
			" ([]functional.Option[T] or a top-level Option); scan into []T, where NULL rows are skipped, " +
			"or use the db/query helpers, which return functional.Option")
	}
}
