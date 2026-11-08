package validate

import (
	"slices"
	"strings"
)

// Codes are the JSON Schema keywords a value failed. A decode error inside a
// union variant uses CodeType or CodeFormat, like a top-level decode error.
const (
	CodeRequired             = "required"
	CodeType                 = "type"
	CodeFormat               = "format"
	CodeEnum                 = "enum"
	CodeConst                = "const"
	CodeMinimum              = "minimum"
	CodeMaximum              = "maximum"
	CodeExclusiveMinimum     = "exclusiveMinimum"
	CodeExclusiveMaximum     = "exclusiveMaximum"
	CodeMultipleOf           = "multipleOf"
	CodeMinLength            = "minLength"
	CodeMaxLength            = "maxLength"
	CodePattern              = "pattern"
	CodeMinItems             = "minItems"
	CodeMaxItems             = "maxItems"
	CodeUniqueItems          = "uniqueItems"
	CodeMinProperties        = "minProperties"
	CodeMaxProperties        = "maxProperties"
	CodeAdditionalProperties = "additionalProperties"
	CodeOneOf                = "oneOf"
	CodeAnyOf                = "anyOf"
	CodeDiscriminator        = "discriminator"
)

// Decoding codes name why a request body is not one valid JSON text. They
// are part of the same client contract as the keyword codes above:
// http/problem.MalformedJSON writes them.
const (
	CodeSyntax        = "syntax"         // not valid JSON
	CodeDuplicateName = "duplicate_name" // an object repeats a member name
	CodeInvalidUTF8   = "invalid_utf8"   // the text is not valid UTF-8
	CodeTrailingData  = "trailing_data"  // data follows the JSON value
)

// Error is one violation: where, which keyword, and a detail for a human. It
// is the domain shape; http/problem.FieldError is the wire shape, and
// problem converts one into the other in a single place.
type Error struct {
	// Pointer is the RFC 6901 JSON Pointer of the offending value; "" is the
	// whole document.
	Pointer string
	// Code is the JSON Schema keyword that failed, one of the Code constants.
	Code string
	// Detail describes the violation for a human, without echoing the value. It
	// is the RFC 9457 "detail" of the wire shape, problem.FieldError.
	Detail string
}

// Errors accumulates violations. The zero value is ready to use.
type Errors []Error

// Add records a violation at p.
func (e *Errors) Add(p Path, code, message string) {
	*e = append(*e, Error{Pointer: p.ptr, Code: code, Detail: message})
}

// Append records every violation in other, in order.
func (e *Errors) Append(other Errors) {
	*e = append(*e, other...)
}

// Error joins the violations into one line, so Errors can travel as an error.
func (e Errors) Error() string {
	var b strings.Builder
	for i, v := range e {
		if i > 0 {
			b.WriteString("; ")
		}
		ptr := v.Pointer
		if ptr == "" {
			ptr = "/"
		}
		b.WriteString(ptr + ": " + v.Detail + " (" + v.Code + ")")
	}
	return b.String()
}

// Required records a required violation when no value exists at p, the path
// of a member. presentFallback answers when p has no document. It reports
// whether the value is present, so callers check it only then.
func (e *Errors) Required(p Path, presentFallback bool) bool {
	if p.Present(presentFallback) {
		return true
	}
	e.Add(p, CodeRequired, "is required")
	return false
}

// NotNull records a type violation when the value at p is a JSON null. It
// reports whether the value may be checked further: true unless it is null.
// Without a document nothing is null.
func (e *Errors) NotNull(p Path) bool {
	if !p.IsNull() {
		return true
	}
	e.Add(p, CodeType, "must not be null")
	return false
}

// OnlyMembers records an additionalProperties violation for every member of
// the object at p whose name is not in allowed, which must be sorted. Without
// a document the Go value cannot hold unknown members, so it records nothing.
func (e *Errors) OnlyMembers(p Path, allowed ...string) {
	names, known := p.Members()
	if !known {
		return
	}
	for _, name := range names {
		if _, found := slices.BinarySearch(allowed, name); !found {
			e.Add(p.Field(name), CodeAdditionalProperties, "is not an allowed property")
		}
	}
}

// MemberCount returns the number of members of the object at p, and whether
// it is known.
func (p Path) MemberCount() (int, bool) {
	names, known := p.Members()
	return len(names), known
}
