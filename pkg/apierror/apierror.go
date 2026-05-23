package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const (
	CodeUnexpected            = "UNEXPECTED_ERROR"
	CodeMissingRequiredScopes = "MISSING_REQUIRED_SCOPES"
	CodeForbidden             = "FORBIDDEN"
	CodeNotFound              = "NOT_FOUND"
	CodeUnauthorized          = "UNAUTHORIZED"
)

// Error represents one or more API-safe error details with one HTTP status.
type Error struct {
	Details []Detail `json:"errors"`
	Status  int      `json:"-"`
}

// Detail is a single machine-readable API error detail.
type Detail struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

var (
	ErrUnexpected = NewWithStatus(CodeUnexpected, "Unexpected error", http.StatusInternalServerError)
	ErrForbidden  = NewWithStatus(CodeForbidden, "Forbidden", http.StatusForbidden)
	ErrNotFound   = NewWithStatus(CodeNotFound, "Not found", http.StatusNotFound)
)

// NewWithDetails creates an Error with explicit details and HTTP status.
func NewWithDetails(details []Detail, status int) *Error {
	return &Error{Details: append([]Detail(nil), details...), Status: status}
}

// NewBadRequest creates an Error with HTTP 400 status.
func NewBadRequest(code, message string) *Error {
	return New(code, message, nil, http.StatusBadRequest)
}

// NewWithStatus creates an Error with a single detail and explicit HTTP status.
func NewWithStatus(code, message string, status int) *Error {
	return New(code, message, nil, status)
}

// New creates an Error with a single detail.
func New(code, message string, metadata map[string]any, status int) *Error {
	return &Error{
		Details: []Detail{{Code: code, Message: message, Metadata: metadata}},
		Status:  status,
	}
}

func (e *Error) Error() string {
	messages := make([]string, 0, len(e.Details))
	for _, detail := range e.Details {
		messages = append(messages, detail.Message)
	}
	return strings.Join(messages, "\n")
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
		items = append(items, ResponseError{
			Code:    detail.Code,
			Message: detail.Message,
			Details: detail.Metadata,
		})
	}
	return Response{Errors: items}
}

// WriteJSON writes Error using the default generated-type-neutral response shape.
func WriteJSON(w http.ResponseWriter, err *Error) {
	if err == nil {
		err = ErrUnexpected
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus())
	_ = json.NewEncoder(w).Encode(ToResponse(err))
}
