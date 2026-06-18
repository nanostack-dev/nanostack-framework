package fault

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	CodeUnexpected            = "UNEXPECTED_ERROR"
	CodeMissingRequiredScopes = "MISSING_REQUIRED_SCOPES"
	CodeBadRequest            = "BAD_REQUEST"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeForbidden             = "FORBIDDEN"
	CodeNotFound              = "NOT_FOUND"
	CodeConflict              = "CONFLICT"
	CodeUnprocessable         = "UNPROCESSABLE_ENTITY"
	CodeTooManyRequests       = "TOO_MANY_REQUESTS"
)

// Error represents one or more API-safe error details with one HTTP status.
//
// Error is immutable from the caller's perspective: the fluent decorators
// (Meta, Metadata, Msgf, Detail, Field, Wrap) each return a copy, so package
// sentinels like ErrNotFound can be decorated without being mutated.
type Error struct {
	Details []Detail `json:"errors"`
	Status  int      `json:"-"`
	// source is the wrapped underlying error, kept unexported so it never
	// leaks into the JSON response. It is surfaced through Unwrap so errors.Is
	// and errors.As reach the original cause, and boundary handlers can log it.
	source error
}

// Detail is a single machine-readable API error detail. Its JSON shape is the
// canonical API error contract: a direct json.Marshal of an Error yields the
// same bytes as ToResponse, so the type can back a generated OpenAPI schema via
// x-go-type without a translation layer. Metadata serializes as "details" to
// match that contract.
type Detail struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Field    string         `json:"field,omitempty"`
	Metadata map[string]any `json:"details,omitempty"`
}

var (
	ErrUnexpected      = NewWithStatus(CodeUnexpected, "Unexpected error", http.StatusInternalServerError)
	ErrBadRequest      = NewWithStatus(CodeBadRequest, "Bad request", http.StatusBadRequest)
	ErrUnauthorized    = NewWithStatus(CodeUnauthorized, "Unauthorized", http.StatusUnauthorized)
	ErrForbidden       = NewWithStatus(CodeForbidden, "Forbidden", http.StatusForbidden)
	ErrNotFound        = NewWithStatus(CodeNotFound, "Not found", http.StatusNotFound)
	ErrConflict        = NewWithStatus(CodeConflict, "Conflict", http.StatusConflict)
	ErrTooManyRequests = NewWithStatus(CodeTooManyRequests, "Too many requests", http.StatusTooManyRequests)
)

// newStatus builds a fresh single-detail Error. Every call allocates so
// package sentinels stay independent of constructed errors.
func newStatus(code, message string, status int) *Error {
	return &Error{
		Details: []Detail{{Code: code, Message: message}},
		Status:  status,
	}
}

// BadRequest builds a 400 API error.
func BadRequest(code, message string) *Error {
	return newStatus(code, message, http.StatusBadRequest)
}

// Unauthorized builds a 401 API error.
func Unauthorized(code, message string) *Error {
	return newStatus(code, message, http.StatusUnauthorized)
}

// Forbidden builds a 403 API error.
func Forbidden(code, message string) *Error {
	return newStatus(code, message, http.StatusForbidden)
}

// NotFound builds a 404 API error.
func NotFound(code, message string) *Error {
	return newStatus(code, message, http.StatusNotFound)
}

// Conflict builds a 409 API error.
func Conflict(code, message string) *Error {
	return newStatus(code, message, http.StatusConflict)
}

// Unprocessable builds a 422 API error.
func Unprocessable(code, message string) *Error {
	return newStatus(code, message, http.StatusUnprocessableEntity)
}

// TooManyRequests builds a 429 API error.
func TooManyRequests(code, message string) *Error {
	return newStatus(code, message, http.StatusTooManyRequests)
}

// Internal builds a 500 API error. Prefer returning a bare error for truly
// unexpected failures so the boundary handler logs them; use Internal only when
// 500 is a modelled, intentional response.
func Internal(code, message string) *Error {
	return newStatus(code, message, http.StatusInternalServerError)
}

// Invalid starts an empty 422 validation error. Attach field-level details with
// Field:
//
//	fault.Invalid().
//		Field("name", "REQUIRED", "name is required").
//		Field("age", "RANGE", "must be positive")
func Invalid() *Error {
	return &Error{Status: http.StatusUnprocessableEntity}
}

// NewWithStatus creates an Error with a single detail and explicit HTTP status.
func NewWithStatus(code, message string, status int) *Error {
	return newStatus(code, message, status)
}

// NewWithDetails creates an Error with explicit details and HTTP status.
func NewWithDetails(details []Detail, status int) *Error {
	return &Error{Details: append([]Detail(nil), details...), Status: status}
}

// New creates an Error with a single detail.
//
// Deprecated: use a semantic constructor with fluent decorators, e.g.
// fault.BadRequest(code, message).Metadata(metadata).
func New(code, message string, metadata map[string]any, status int) *Error {
	return &Error{
		Details: []Detail{{Code: code, Message: message, Metadata: metadata}},
		Status:  status,
	}
}

// NewBadRequest creates an Error with HTTP 400 status.
//
// Deprecated: use fault.BadRequest(code, message).
func NewBadRequest(code, message string) *Error {
	return BadRequest(code, message)
}

// NewBadRequestWithMetadata creates an Error with HTTP 400 status and metadata.
//
// Deprecated: use fault.BadRequest(code, message).Metadata(metadata).
func NewBadRequestWithMetadata(code, message string, metadata map[string]any) *Error {
	return BadRequest(code, message).Metadata(metadata)
}

// clone returns a shallow copy with an independent Details slice so decorators
// never mutate the receiver (including package sentinels).
func (e *Error) clone() *Error {
	if e == nil {
		e = ErrUnexpected
	}
	cp := *e
	cp.Details = append([]Detail(nil), e.Details...)
	return &cp
}

// ensurePrimary guarantees there is at least one detail to decorate.
func (e *Error) ensurePrimary() {
	if len(e.Details) == 0 {
		e.Details = []Detail{{}}
	}
}

// Meta returns a copy with key/value added to the primary detail's metadata.
func (e *Error) Meta(key string, value any) *Error {
	c := e.clone()
	c.ensurePrimary()
	last := len(c.Details) - 1
	meta := make(map[string]any, len(c.Details[last].Metadata)+1)
	for k, v := range c.Details[last].Metadata {
		meta[k] = v
	}
	meta[key] = value
	c.Details[last].Metadata = meta
	return c
}

// Metadata returns a copy with the given map merged into the primary detail's
// metadata.
func (e *Error) Metadata(metadata map[string]any) *Error {
	c := e.clone()
	c.ensurePrimary()
	last := len(c.Details) - 1
	meta := make(map[string]any, len(c.Details[last].Metadata)+len(metadata))
	for k, v := range c.Details[last].Metadata {
		meta[k] = v
	}
	for k, v := range metadata {
		meta[k] = v
	}
	c.Details[last].Metadata = meta
	return c
}

// Msgf returns a copy whose primary detail message is formatted.
func (e *Error) Msgf(format string, args ...any) *Error {
	c := e.clone()
	c.ensurePrimary()
	c.Details[len(c.Details)-1].Message = fmt.Sprintf(format, args...)
	return c
}

// Detail returns a copy with an additional error detail appended.
func (e *Error) Detail(code, message string) *Error {
	c := e.clone()
	c.Details = append(c.Details, Detail{Code: code, Message: message})
	return c
}

// Field returns a copy with an additional field-scoped detail appended. Use it
// to build validation errors that map onto ResponseError.Field.
func (e *Error) Field(field, code, message string) *Error {
	c := e.clone()
	c.Details = append(c.Details, Detail{Field: field, Code: code, Message: message})
	return c
}

// Wrap returns a copy carrying err as the underlying cause. The cause is never
// serialized, but errors.Is/errors.As reach it and boundary handlers can log
// it with full detail.
func (e *Error) Wrap(err error) *Error {
	c := e.clone()
	c.source = err
	return c
}

// Unwrap exposes the wrapped cause attached by Wrap.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.source
}

// Is matches by HTTP status so errors.Is(err, fault.ErrNotFound) reports
// whether err is a 404-class API error regardless of its specific code.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.HTTPStatus() == other.HTTPStatus()
}

func (e *Error) Error() string {
	if e == nil {
		return ErrUnexpected.Error()
	}
	messages := make([]string, 0, len(e.Details))
	for _, detail := range e.Details {
		messages = append(messages, detail.Message)
	}
	msg := strings.Join(messages, "\n")
	if e.source != nil {
		if msg == "" {
			return e.source.Error()
		}
		return msg + ": " + e.source.Error()
	}
	return msg
}

// Message returns the primary client-safe message. Unlike Error it never
// includes the wrapped cause, so it is safe to expose at transport boundaries
// (e.g. an SSE error frame).
func (e *Error) Message() string {
	if e == nil || len(e.Details) == 0 {
		return ErrUnexpected.Details[0].Message
	}
	return e.Details[0].Message
}

// HTTPStatus returns the HTTP status carried by the error.
func (e *Error) HTTPStatus() int {
	if e == nil || e.Status == 0 {
		return http.StatusInternalServerError
	}
	return e.Status
}

// StatusCode preserves compatibility with existing HTTP-status-based error handling.
func (e *Error) StatusCode() int {
	return e.HTTPStatus()
}

// As returns the framework API error when err wraps one.
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// Response is the default framework error response shape.
type Response struct {
	Errors []ResponseError `json:"errors"`
}

// ResponseError is generated-type neutral so apps can map into their own DTOs.
type ResponseError struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
	Field   *string        `json:"field,omitempty"`
	Message string         `json:"message"`
}

// ToResponse maps Error into the default generated-type-neutral response shape.
func ToResponse(err *Error) Response {
	if err == nil {
		err = ErrUnexpected
	}
	items := make([]ResponseError, 0, len(err.Details))
	for _, detail := range err.Details {
		item := ResponseError{
			Code:    detail.Code,
			Message: detail.Message,
			Details: detail.Metadata,
		}
		if detail.Field != "" {
			field := detail.Field
			item.Field = &field
		}
		items = append(items, item)
	}
	return Response{Errors: items}
}

// WriteJSON writes Error as the canonical API error contract. Error marshals to
// that contract directly, so the boundary, a direct json.Marshal, and a
// generated x-go-type'd schema all emit identical bytes.
func WriteJSON(w http.ResponseWriter, err *Error) {
	if err == nil {
		err = ErrUnexpected
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus())
	_ = json.NewEncoder(w).Encode(err)
}
