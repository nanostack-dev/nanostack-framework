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

// ClassificationKind identifies how an error should be handled at API
// boundaries.
type ClassificationKind string

const (
	// KindHandledAPI is a framework API error that is safe to return to the
	// caller with its own status and details.
	KindHandledAPI ClassificationKind = "handled_api"
	// KindReportedUnexpected is an internal failure that was already logged near
	// the source, so boundary handlers should return a generic 500 without
	// emitting another error log.
	KindReportedUnexpected ClassificationKind = "reported_unexpected"
	// KindUnexpected is an internal failure that has not been source-reported.
	KindUnexpected ClassificationKind = "unexpected"
)

// Classification is the framework's boundary decision for an error.
type Classification struct {
	Kind     ClassificationKind
	Err      error
	APIError *Error
	Status   int
}

// Handled reports whether the error is safe to expose through the API.
func (c Classification) Handled() bool {
	return c.Kind == KindHandledAPI
}

// ReportedUnexpected reports whether the error is an internal failure already
// logged close to its source.
func (c Classification) ReportedUnexpected() bool {
	return c.Kind == KindReportedUnexpected
}

// Unexpected reports whether the error is an internal failure that still needs
// boundary logging.
func (c Classification) Unexpected() bool {
	return c.Kind == KindUnexpected
}

// ReportedUnexpectedError marks an internal error as already logged close to
// its source while preserving normal errors.Is/errors.As unwrapping.
type ReportedUnexpectedError struct {
	Err error
}

func (e *ReportedUnexpectedError) Error() string {
	if e == nil || e.Err == nil {
		return "reported unexpected error"
	}
	return e.Err.Error()
}

func (e *ReportedUnexpectedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MarkReportedUnexpected marks err as an internal failure that has already
// been logged close to its source. API errors are returned unchanged because
// they are handled responses, not unexpected failures.
func MarkReportedUnexpected(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := As(err); ok {
		return err
	}
	if IsReportedUnexpected(err) {
		return err
	}
	return &ReportedUnexpectedError{Err: err}
}

// IsReportedUnexpected reports whether err has been marked with
// MarkReportedUnexpected.
func IsReportedUnexpected(err error) bool {
	var reported *ReportedUnexpectedError
	return errors.As(err, &reported)
}

// Classify maps err into the framework's API boundary handling model.
func Classify(err error) Classification {
	if apiErr, ok := As(err); ok && apiErr != nil {
		return Classification{
			Kind:     KindHandledAPI,
			Err:      err,
			APIError: apiErr,
			Status:   apiErr.HTTPStatus(),
		}
	}

	if IsReportedUnexpected(err) {
		return Classification{
			Kind:     KindReportedUnexpected,
			Err:      err,
			APIError: ErrUnexpected,
			Status:   http.StatusInternalServerError,
		}
	}

	return Classification{
		Kind:     KindUnexpected,
		Err:      err,
		APIError: ErrUnexpected,
		Status:   http.StatusInternalServerError,
	}
}

// NewWithDetails creates an Error with explicit details and HTTP status.
func NewWithDetails(details []Detail, status int) *Error {
	return &Error{Details: append([]Detail(nil), details...), Status: status}
}

// NewBadRequest creates an Error with HTTP 400 status.
func NewBadRequest(code, message string) *Error {
	return New(code, message, nil, http.StatusBadRequest)
}

// NewBadRequestWithMetadata creates an Error with HTTP 400 status and metadata.
func NewBadRequestWithMetadata(code, message string, metadata map[string]any) *Error {
	return New(code, message, metadata, http.StatusBadRequest)
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
