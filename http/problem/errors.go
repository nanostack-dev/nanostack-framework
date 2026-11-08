package problem

import (
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

var (
	_ fault.Coded    = (*Error)(nil)
	_ fault.Detailed = (*Error)(nil)
)

// Error is a framework problem. It implements fault.Coded and
// fault.Detailed, and carries the field errors and the cause for logs.
type Error struct {
	typ    string
	title  string
	status int
	code   string
	detail string
	errs   []FieldError
	cause  error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.code)
	if e.detail != "" {
		b.WriteString(": " + e.detail)
	}
	for _, f := range e.errs {
		loc := f.Pointer
		if f.Parameter != "" {
			loc = f.Parameter
		}
		b.WriteString("; " + loc + " " + f.Code)
	}
	if e.cause != nil {
		b.WriteString(": " + e.cause.Error())
	}
	return b.String()
}

// Unwrap returns the cause, for logs and errors.Is.
func (e *Error) Unwrap() error { return e.cause }

// ProblemType implements fault.Described.
func (e *Error) ProblemType() string { return e.typ }

// ProblemStatus implements fault.Described.
func (e *Error) ProblemStatus() int { return e.status }

// ProblemTitle implements fault.Described.
func (e *Error) ProblemTitle() string { return e.title }

// ProblemDetail implements fault.Detailed.
func (e *Error) ProblemDetail() string { return e.detail }

// ProblemCode implements fault.Coded.
func (e *Error) ProblemCode() string { return e.code }

// FieldErrors returns the violations the problem lists.
func (e *Error) FieldErrors() []FieldError { return e.errs }

func (e *Error) problemErrors() []FieldError { return e.errs }

// Validation is the 422 problem for a body that decoded but violates the
// schema. It lists every violation.
func Validation(errs validate.Errors) error {
	return &Error{
		typ: TypeValidation, title: "Request validation failed", status: http.StatusUnprocessableEntity,
		code: "http.validation_failed", detail: plural(len(errs), "violation") + " in the request body.",
		errs: fieldErrors(errs, inBody),
	}
}

// InvalidBody is the 400 problem for a body that is valid JSON but cannot
// become the request type: a value of the wrong JSON type, an integer out of
// range or a malformed date-time. pointer locates the value; cause is the
// json/v2 error, from which the code and detail are derived.
func InvalidBody(pointer string, cause error) error {
	f := FieldError{Pointer: pointer, Code: validate.CodeType, Detail: "has an invalid value"}
	if e, ok := validate.DecodeError(validate.Root, cause); ok {
		f = fieldError(e, inBody)
		f.Pointer = pointer
	}
	return &Error{
		typ: TypeInvalidBody, title: "Invalid request body", status: http.StatusBadRequest,
		code: "http.invalid_body", detail: "The request body does not match the expected types.",
		errs: []FieldError{f}, cause: cause,
	}
}

// MalformedJSON is the 400 problem for a body that is not one valid JSON
// text: a syntax error, a duplicate member name, invalid UTF-8 or trailing
// data. The pointer of a syntactic error is kept.
func MalformedJSON(cause error) error {
	f := FieldError{Code: validate.CodeSyntax, Detail: "is not valid JSON"}
	if se, ok := errors.AsType[*jsontext.SyntacticError](cause); ok {
		f.Pointer = string(se.JSONPointer)
		switch {
		case errors.Is(se.Err, jsontext.ErrDuplicateName):
			f.Code, f.Detail = validate.CodeDuplicateName, "repeats an object member name"
		case se.Err != nil && se.Err.Error() == "invalid UTF-8":
			// jsontext does not export this sentinel; its text is stable.
			f.Code, f.Detail = validate.CodeInvalidUTF8, "contains invalid UTF-8"
		default:
			f.Detail = "is not valid JSON at byte offset " + strconv.FormatInt(se.ByteOffset, 10)
			if strings.Contains(se.Error(), "after top-level value") {
				f.Code, f.Detail = validate.CodeTrailingData, "has data after the JSON value"
			}
		}
	}
	return &Error{
		typ: TypeMalformedJSON, title: "Malformed JSON", status: http.StatusBadRequest,
		code: "http.malformed_json", detail: "The request body is not valid JSON.",
		errs: []FieldError{f}, cause: cause,
	}
}

// MissingBody is the 400 problem for an empty body where one is required.
func MissingBody() error {
	return &Error{
		typ: TypeMissingBody, title: "Missing request body", status: http.StatusBadRequest,
		code: "http.missing_body", detail: "The request requires a JSON body.",
	}
}

// BadRequest is the 400 problem for a request that cannot be read for a
// reason no other problem names, such as a form or multipart body that does
// not parse. It does not echo cause, which it keeps for logs.
func BadRequest(cause error) error {
	return &Error{
		typ: TypeBadRequest, title: "Bad request", status: http.StatusBadRequest,
		code: "http.bad_request", detail: "The request could not be read.",
		cause: cause,
	}
}

// BodyTooLarge is the 413 problem for a body over limit bytes.
func BodyTooLarge(limit int64) error {
	return &Error{
		typ: TypeBodyTooLarge, title: "Request body too large", status: http.StatusRequestEntityTooLarge,
		code: "http.body_too_large", detail: "The request body exceeds " + strconv.FormatInt(limit, 10) + " bytes.",
	}
}

// UnsupportedMediaType is the 415 problem for a body that is not JSON.
// mediaType is the parsed media type, such as text/plain, without its
// parameters: the detail names it, and never echoes the raw header.
func UnsupportedMediaType(mediaType string) error {
	return &Error{
		typ: TypeUnsupportedMediaType, title: "Unsupported media type", status: http.StatusUnsupportedMediaType,
		code:   "http.unsupported_media_type",
		detail: "The request body must be application/json, not " + strconv.Quote(mediaType) + ".",
	}
}

// InvalidParameter is the 400 problem for a path, query, header or cookie
// parameter that is missing or cannot be parsed. The cause is logged, not
// sent.
func InvalidParameter(name string, cause error) error {
	detail := "is invalid"
	if cause == nil {
		detail = "is required"
	}
	code := validate.CodeType
	if cause == nil {
		code = validate.CodeRequired
	}
	return &Error{
		typ: TypeInvalidParameter, title: "Invalid request parameter", status: http.StatusBadRequest,
		code: "http.invalid_parameter", detail: "A request parameter is missing or invalid.",
		errs: []FieldError{{Parameter: name, Code: code, Detail: detail}}, cause: cause,
	}
}

// InvalidParameters is the 400 problem for parameters that parsed but
// violate their schema, such as limit=500 with maximum 100. The violations'
// pointers name the parameters ("/limit").
func InvalidParameters(errs validate.Errors) error {
	return &Error{
		typ: TypeInvalidParameter, title: "Invalid request parameter", status: http.StatusBadRequest,
		code: "http.invalid_parameter", detail: plural(len(errs), "violation") + " in the request parameters.",
		errs: fieldErrors(errs, inParameters),
	}
}

// location says where a core/validate violation was found.
type location int

const (
	inBody       location = iota // Pointer is a JSON Pointer into the body
	inParameters                 // Pointer names the parameter, as "/limit"
)

// fieldError converts a core/validate violation, the domain shape, to a
// FieldError, the wire shape. It is the only conversion between the two.
func fieldError(v validate.Error, at location) FieldError {
	if at == inParameters {
		name := validate.UnescapeToken(strings.TrimPrefix(v.Pointer, "/"))
		return FieldError{Parameter: name, Code: v.Code, Detail: v.Detail}
	}
	return FieldError{Pointer: v.Pointer, Code: v.Code, Detail: v.Detail}
}

func fieldErrors(errs validate.Errors, at location) []FieldError {
	fields := make([]FieldError, len(errs))
	for i, v := range errs {
		fields[i] = fieldError(v, at)
	}
	return fields
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
