// Package validate is the runtime of the validators that nano gen validate
// generates from an OpenAPI document.
//
// A generated validator has one signature:
//
//	func (r T) Validate(p validate.Path) validate.Errors
//
// It accumulates every violation, each with an RFC 6901 JSON Pointer, a code
// and a message. The code is the JSON Schema keyword that failed (required,
// minLength, pattern, oneOf, ...), so a client can map it back to the
// contract.
//
// A typed Go value loses two facts that JSON Schema depends on: whether a
// member was present, and whether it was null. A Path built by [RootOf] from
// the request body keeps them, so required, null and additionalProperties
// checks are exact. [Root] has no document; validators then fall back to what
// the Go value can tell (a nil pointer is absent).
//
// The package has no dependency on net/http and walks no spec at runtime.
package validate

import (
	"strconv"
	"strings"
)

// Path is the location of a value inside a JSON document, rendered as an
// RFC 6901 JSON Pointer. It is an immutable value: Field and Index return a
// new Path and share the document, if any.
type Path struct {
	ptr string
	doc *document
}

// Root is the path of the top-level value with no document attached. Use it
// to validate a value built in Go; use [RootOf] for a decoded request body.
var Root = Path{}

// RootOf returns the root path of the JSON text data. Validators use the
// attached document to tell an absent member from a zero value and a null
// from an absent member. data must be the exact text the value was decoded
// from; it is indexed lazily, on the first presence query.
func RootOf(data []byte) Path {
	return Path{doc: newDocument(data)}
}

// Field returns the path of the object member name.
func (p Path) Field(name string) Path {
	return Path{ptr: p.ptr + "/" + EscapeToken(name), doc: p.doc}
}

// Index returns the path of the array element i.
func (p Path) Index(i int) Path {
	return Path{ptr: p.ptr + "/" + strconv.Itoa(i), doc: p.doc}
}

// Join returns the path of the value at the relative JSON Pointer rel, such
// as the pointer of a decode error inside a nested value. rel must be empty
// or start with "/".
func (p Path) Join(rel string) Path {
	return Path{ptr: p.ptr + rel, doc: p.doc}
}

// Pointer returns the RFC 6901 JSON Pointer of the path. The root is "".
func (p Path) Pointer() string { return p.ptr }

// String returns the JSON Pointer of the path.
func (p Path) String() string { return p.ptr }

// HasDocument reports whether the path carries the JSON document it points
// into, so that presence and null are known.
func (p Path) HasDocument() bool { return p.doc != nil && p.doc.ok() }

// Present reports whether a value exists at the path. With a document the
// answer is exact. Without one it returns fallback, which generated code
// derives from the Go value (for example, a non-nil pointer).
func (p Path) Present(fallback bool) bool {
	if k, known := p.kind(); known {
		return k != 0
	}
	return fallback
}

// IsNull reports whether the value at the path is a JSON null. Without a
// document it returns false.
func (p Path) IsNull() bool {
	k, known := p.kind()
	return known && k == 'n'
}

// Members returns the member names of the object at the path, in document
// order, and whether they are known. Without a document they are not.
func (p Path) Members() ([]string, bool) {
	if !p.HasDocument() {
		return nil, false
	}
	if n := p.doc.lookup(p.ptr); n != nil {
		return n.names, true
	}
	return nil, true
}

// kind returns the JSON kind of the value at the path, 0 when there is none,
// and whether it is known.
func (p Path) kind() (byte, bool) {
	if !p.HasDocument() {
		return 0, false
	}
	if n := p.doc.lookup(p.ptr); n != nil {
		return n.kind, true
	}
	return 0, true
}

// EscapeToken escapes one JSON Pointer reference token as RFC 6901 section 3
// requires: "~" becomes "~0" and "/" becomes "~1". It is the one copy of the
// rule: http/problem and gen/validate use it too.
func EscapeToken(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}
