// Package optional recognises functional.Option[T] by reflection, so that qrm and the
// SQL builder can handle it without importing pkg/functional.
//
// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.
package optional

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// PkgPath is the import path of the package that declares Option.
const PkgPath = "github.com/nanostack-dev/nanostack-framework/pkg/functional"

type info struct {
	ok   bool
	elem reflect.Type
	get  int // index of the method Get() (T, bool) in the value method set
}

var cache sync.Map // reflect.Type -> info

func lookup(t reflect.Type) info {
	if t == nil {
		return info{}
	}
	if cached, ok := cache.Load(t); ok {
		if hit, isInfo := cached.(info); isInfo {
			return hit
		}
	}
	res := info{}
	if t.Kind() == reflect.Struct && t.PkgPath() == PkgPath && strings.HasPrefix(t.Name(), "Option[") {
		value, hasValue := t.MethodByName("Value")
		get, hasGet := t.MethodByName("Get")
		if hasValue && hasGet && value.Type.NumOut() == 1 {
			res = info{ok: true, elem: value.Type.Out(0), get: get.Index}
		}
	}
	cache.Store(t, res)
	return res
}

// Elem reports whether t is functional.Option[E] and returns E.
//
// The match is exact: a struct type named Option[...] declared in PkgPath. A structural
// rule (any type with IsPresent and Value) would also catch unrelated types, and a
// rename of Option breaks the match loudly in optional_test.go.
func Elem(t reflect.Type) (elem reflect.Type, ok bool) {
	i := lookup(t)
	return i.elem, i.ok
}

// IsOption reports whether t is functional.Option[E] for some E.
func IsOption(t reflect.Type) bool {
	return lookup(t).ok
}

// ElemOrSelf returns E for Option[E] and t unchanged otherwise.
func ElemOrSelf(t reflect.Type) reflect.Type {
	if elem, ok := Elem(t); ok {
		return elem
	}
	return t
}

// presenceReporter is implemented by Option; it keeps reflection off the path of
// ordinary arguments.
type presenceReporter interface{ IsPresent() bool }

// Unwrap returns the value an Option carries, or nil when it is absent.
// ok is false when v is not an Option; v is then returned unchanged.
// An Option that carries another Option is unwrapped again, so that absence at any
// level gives nil.
func Unwrap(v any) (value any, ok bool) {
	if _, maybe := v.(presenceReporter); !maybe {
		return v, false
	}
	if !lookup(reflect.TypeOf(v)).ok {
		return v, false
	}
	for {
		inner, present := Get(reflect.ValueOf(v))
		if !present {
			return nil, true
		}
		v = inner.Interface()
		if _, maybe := v.(presenceReporter); !maybe || !lookup(inner.Type()).ok {
			return v, true
		}
	}
}

// Get returns the value of the Option held in v and whether it is present.
// It panics when v does not hold an Option.
func Get(v reflect.Value) (reflect.Value, bool) {
	i := lookup(v.Type())
	if !i.ok {
		panic(fmt.Sprintf("optional: %s is not a functional.Option", v.Type()))
	}
	out := v.Method(i.get).Call(nil)
	return out[0], out[1].Bool()
}

// Set stores x, a reflect.Value of type E, into the addressable Option[E] at dst by
// calling (*Option[E]).Scan(x.Interface()). It relies on Scan's same-type path, so it
// never converts.
func Set(dst, x reflect.Value) error {
	elem, ok := Elem(dst.Type())
	if !ok {
		panic(fmt.Sprintf("optional: %s is not a functional.Option", dst.Type()))
	}
	if x.Type() != elem {
		panic(fmt.Sprintf("optional: can not store %s in %s", x.Type(), dst.Type()))
	}
	scanner, ok := reflect.TypeAssert[sql.Scanner](dst.Addr())
	if !ok {
		panic(fmt.Sprintf("optional: %s does not implement sql.Scanner", dst.Type()))
	}
	return scanner.Scan(x.Interface())
}
