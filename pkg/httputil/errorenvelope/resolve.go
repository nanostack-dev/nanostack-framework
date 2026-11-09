package errorenvelope

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	corefault "github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
)

// Codes of the envelope that this package writes.
const (
	// CodeBadRequest is the default code of a boundary error (Options.BoundaryCode),
	// and the code of a search page or filter over a limit of db/search.
	CodeBadRequest = "BAD_REQUEST"
	// CodeInvalidCursor is the code of a pagination cursor that cannot continue
	// the search: malformed, issued for another sort, or sent with an offset.
	CodeInvalidCursor = "INVALID_CURSOR"
	// CodeInvalidSort is the code of a sort field the search cannot order by,
	// and of a sort direction other than ASC or DESC.
	CodeInvalidSort = "INVALID_SORT"
	// CodePayloadTooLarge is the code of a body over the size limit (413).
	CodePayloadTooLarge = "PAYLOAD_TOO_LARGE"
	// CodeUnsupportedMediaType is the code of a body that is not JSON (415).
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	// CodeNotFound is the code of NotFound, a path that matches no route.
	CodeNotFound = "NOT_FOUND"
	// CodeMethodNotAllowed is the code of MethodNotAllowed.
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
)

// StatusClientClosedRequest is the nginx convention for a request the client
// abandoned before the response was written: problem.StatusClientClosedRequest.
const StatusClientClosedRequest = problem.StatusClientClosedRequest

// Metadata keys of a boundary error detail.
const (
	MetaField = "field" // the JSON Pointer into the body, or the parameter name
	MetaRule  = "rule"  // the JSON Schema keyword or decoding code that failed
)

// Options configures the resolution and the Writer.
type Options struct {
	// Logger receives the "request failed" line of the Writer. Nil means
	// slog.Default(), as http/problem.Write.
	Logger *slog.Logger
	// BoundaryCode is the code of a 400 boundary error: a body that is not
	// valid JSON, a missing body, an invalid parameter or a validation
	// failure. Empty means CodeBadRequest.
	BoundaryCode string
	// Adapter converts the service's own error types to a pkg/fault error,
	// as modules/httpserver.APIErrorAdapter. Nil means none.
	Adapter func(error) (*fault.Error, bool)
	// IsStream reports whether the request is a server-sent event stream. An
	// error on such a request is written as an "event: error" frame.
	IsStream func(*http.Request) bool
	// Log, when set, writes the log lines of the Writer in place of its
	// "request failed" line. The Writer calls it once per error, after the
	// response is written. A service that keeps its own lines passes them
	// here; StrictErrorHandlerLog returns the lines of
	// modules/httpserver.StrictErrorHandler, and a func that does nothing
	// logs nothing. Resolve and ResolveRequest do not call it.
	Log func(r *http.Request, f Failure)
}

// Source names the Writer method that received an error.
type Source int

const (
	// SourceResponse is ResponseError: an error that a handler returned.
	SourceResponse Source = iota
	// SourceRequest is RequestError: body decoding, generated validation or
	// parameter binding, before the handler ran.
	SourceRequest
	// SourceRouter is Handler, NotFound or MethodNotAllowed: no operation
	// matched the request.
	SourceRouter
)

// Failure is one error that the Writer answered, as Options.Log receives it.
type Failure struct {
	// Err is the error the Writer received.
	Err error
	// Source is the Writer method that received it.
	Source Source
	// Response is the resolution that the Writer wrote.
	Response Response
	// SentStatus is the status the client received: Response.Status, or,
	// for a stream that had already sent its status, that status.
	SentStatus int
	// Written is false when the stream error frame was not written because
	// the response had started with another Content-Type (see
	// Response.Write).
	Written bool
}

// Response is the resolution of one error: what to write.
type Response struct {
	// Status is the HTTP status. It is 0 for a stream that the client closed:
	// nothing is written.
	Status int
	// Body is the envelope. Nil means no body: a request that the client
	// abandoned (499), or a stream that the client closed.
	Body *fault.Error
	// Stream reports that the error is written as a server-sent event frame.
	Stream bool
	// Unmodelled reports an error that carries no status, code or message of
	// its own: Body is the generic 500, and the cause must be logged in full.
	Unmodelled bool
	// Abandoned reports a request that the client abandoned (499), or a
	// stream that the client closed.
	Abandoned bool
}

// Resolve maps err, which a handler returned (the response hook), to the
// Response to write. In order:
//
//   - a request that the client abandoned: 499, no body. A cancelled request
//     often also carries a fault from the layer that gave up, and the
//     cancellation is the truer story. An expired deadline is not a cancel:
//     the framework sets no deadline, so it is a timeout of the service.
//   - a *pkg/fault.Error, or what Options.Adapter returns: its status and its
//     errors, byte-identical to fault.WriteJSON.
//   - a search client error of db/search:
//     ErrInvalidCursor or ErrCursorWithOffset is 400 INVALID_CURSOR (field
//     pagination.cursor); ErrUnknownSort is 400 INVALID_SORT (field sort_by);
//     ErrInvalidSortDirection is 400 INVALID_SORT (field sort_direction);
//     ErrInvalidLimit or ErrInvalidOffset is 400 BAD_REQUEST (field
//     pagination.limit or pagination.offset); ErrTooManyValues,
//     ErrTermTooLong or ErrInvalidTime is 400 BAD_REQUEST with no field.
//     db/search.ErrUnscoped
//     is a programming error: it is not mapped, so it is the 500 below.
//   - an http/problem boundary error: as ResolveRequest.
//   - a core/fault.Described, such as a generated catalogue error: its status,
//     code ProblemCode() in upper case (invoice_not_found becomes
//     INVOICE_NOT_FOUND), message ProblemDetail(), or the title when there is
//     no detail.
//   - anything else: 500 UNEXPECTED_ERROR "Unexpected error", Unmodelled.
//
// When opts.IsStream reports a stream, the Response is a stream frame with the
// same status and message, and a cancelled or expired stream writes nothing.
func Resolve(r *http.Request, err error, opts Options) Response {
	if opts.IsStream != nil && r != nil && opts.IsStream(r) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Response{Stream: true, Abandoned: true}
		}
		resp := resolveHandlerError(err, opts)
		resp.Stream = true
		return resp
	}
	if clientAbandoned(r, err) {
		return Response{Status: StatusClientClosedRequest, Abandoned: true}
	}
	return resolveHandlerError(err, opts)
}

// ResolveRequest maps err, which the request hook received (body decoding,
// generated validation, parameter binding), to the Response to write.
//
// An http/problem boundary error becomes:
//
//   - 413 PAYLOAD_TOO_LARGE and 415 UNSUPPORTED_MEDIA_TYPE with the problem's
//     detail, and 404 NOT_FOUND and 405 METHOD_NOT_ALLOWED.
//   - Any other status, 422 included, becomes 400 with one detail per field
//     error: code Options.BoundaryCode, message "<location>: <detail>" (the
//     detail alone when the location is the body root), metadata
//     {"field": location, "rule": code}. The input value is never echoed.
//
// Any other error, such as the "can't decode JSON body" error of
// oapi-codegen's chi strict server, is 400 Options.BoundaryCode with the
// error text, as modules/httpserver.StrictErrorHandler answers it in v0.22.0.
func ResolveRequest(_ *http.Request, err error, opts Options) Response {
	if p, ok := errors.AsType[*problem.Error](err); ok {
		return Response{Status: boundaryStatus(p), Body: boundaryFault(p, opts)}
	}
	message := http.StatusText(http.StatusBadRequest)
	if err != nil {
		message = err.Error()
	}
	return Response{Status: http.StatusBadRequest, Body: fault.BadRequest(boundaryCode(opts), message)}
}

func resolveHandlerError(err error, opts Options) Response {
	if apiErr, ok := faultFrom(err, opts); ok {
		return Response{Status: apiErr.HTTPStatus(), Body: apiErr}
	}
	if p, ok := errors.AsType[*problem.Error](err); ok {
		return Response{Status: boundaryStatus(p), Body: boundaryFault(p, opts)}
	}
	if d, ok := errors.AsType[corefault.Described](err); ok && corefault.Valid(d) {
		apiErr := describedFault(d)
		return Response{Status: apiErr.HTTPStatus(), Body: apiErr}
	}
	return Response{Status: http.StatusInternalServerError, Body: fault.ErrUnexpected, Unmodelled: true}
}

// clientAbandoned reports whether the request failed because its context was
// cancelled, which net/http does when the client disconnects.
func clientAbandoned(r *http.Request, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return r != nil && errors.Is(r.Context().Err(), context.Canceled)
}

// faultFrom recovers a pkg/fault error from err, directly, from the search
// sentinels, or through the adapter.
func faultFrom(err error, opts Options) (*fault.Error, bool) {
	if apiErr, ok := fault.As(err); ok {
		return apiErr, true
	}
	if apiErr, ok := searchFault(err); ok {
		return apiErr, true
	}
	if opts.Adapter != nil {
		if apiErr, ok := opts.Adapter(err); ok && apiErr != nil {
			return apiErr, true
		}
	}
	return nil, false
}

// searchFault maps the client errors of a search to a 400, so every search
// endpoint answers a bad page, sort or filter the same way. The checks run
// from the most specific error to the most general one. ErrUnscoped wins over
// every client error: an error that holds it (for example through errors.Join)
// is not mapped, so it is the unmodelled 500.
func searchFault(err error) (*fault.Error, bool) {
	if errors.Is(err, search.ErrUnscoped) {
		return nil, false
	}
	var message string
	code, field := CodeInvalidCursor, "pagination.cursor"
	switch {
	case errors.Is(err, search.ErrUnknownSort):
		code, field = CodeInvalidSort, "sort_by"
		message = "The search cannot sort by this field."
	case errors.Is(err, search.ErrInvalidSortDirection):
		code, field = CodeInvalidSort, "sort_direction"
		message = "The sort direction must be ASC or DESC."
	case errors.Is(err, search.ErrInvalidLimit):
		code, field = CodeBadRequest, "pagination.limit"
		message = fmt.Sprintf("The page size must be between 1 and %d.", search.MaxLimit)
	case errors.Is(err, search.ErrInvalidOffset):
		code, field = CodeBadRequest, "pagination.offset"
		message = "The offset cannot be negative."
	case errors.Is(err, search.ErrTooManyValues):
		code, field = CodeBadRequest, ""
		message = fmt.Sprintf("A filter has more than %d values.", search.MaxInValues)
	case errors.Is(err, search.ErrTermTooLong):
		code, field = CodeBadRequest, ""
		message = fmt.Sprintf("A search term is longer than %d characters.", search.MaxTermLength)
	case errors.Is(err, search.ErrInvalidTime):
		code, field = CodeBadRequest, ""
		message = "A time filter must be from 0001-01-02 to 9999-12-30, in UTC."
	case errors.Is(err, search.ErrCursorWithOffset):
		message = "A cursor cannot be combined with an offset."
	case errors.Is(err, search.ErrInvalidCursor):
		message = "The cursor is invalid for this search. Restart without a cursor, " +
			"or send back the next_cursor of the previous page with the same sort."
	default:
		return nil, false
	}
	apiErr := &fault.Error{
		Details: []fault.Detail{{Code: code, Message: message, Field: field}},
		Status:  http.StatusBadRequest,
	}
	return apiErr.Wrap(err), true
}

// describedFault renders a core/fault.Described as one envelope detail.
func describedFault(d corefault.Described) *fault.Error {
	code := statusCode(d.ProblemStatus())
	if c, ok := d.(corefault.Coded); ok && c.ProblemCode() != "" {
		code = strings.ToUpper(c.ProblemCode())
	}
	message := d.ProblemTitle()
	if dd, ok := d.(corefault.Detailed); ok && dd.ProblemDetail() != "" {
		message = dd.ProblemDetail()
	}
	return fault.NewWithStatus(code, message, d.ProblemStatus()).Wrap(d)
}

func boundaryCode(opts Options) string {
	if opts.BoundaryCode != "" {
		return opts.BoundaryCode
	}
	return CodeBadRequest
}

// boundaryStatus keeps the statuses that the envelope contract has (413, 415,
// 404, 405) and answers every other boundary error with 400, as the services
// answer a request error today.
func boundaryStatus(p *problem.Error) int {
	switch status := p.ProblemStatus(); status {
	case http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
		http.StatusNotFound, http.StatusMethodNotAllowed:
		return status
	}
	return http.StatusBadRequest
}

func boundaryFault(p *problem.Error, opts Options) *fault.Error {
	status := boundaryStatus(p)
	switch status {
	case http.StatusRequestEntityTooLarge:
		return fault.NewWithStatus(CodePayloadTooLarge, p.ProblemDetail(), status).Wrap(p)
	case http.StatusUnsupportedMediaType:
		return fault.NewWithStatus(CodeUnsupportedMediaType, p.ProblemDetail(), status).Wrap(p)
	case http.StatusNotFound:
		return fault.NewWithStatus(CodeNotFound, p.ProblemDetail(), status).Wrap(p)
	case http.StatusMethodNotAllowed:
		return fault.NewWithStatus(CodeMethodNotAllowed, p.ProblemDetail(), status).Wrap(p)
	}
	code := boundaryCode(opts)
	fields := p.FieldErrors()
	if len(fields) == 0 {
		return fault.NewWithStatus(code, p.ProblemDetail(), status).Wrap(p)
	}
	details := make([]fault.Detail, len(fields))
	for i, f := range fields {
		details[i] = fieldDetail(code, f)
	}
	return fault.NewWithDetails(details, status).Wrap(p)
}

// fieldDetail renders one field error. The location is the parameter name or
// the JSON Pointer into the body; "" is the body root.
func fieldDetail(code string, f problem.FieldError) fault.Detail {
	location := f.Pointer
	if f.Parameter != "" {
		location = f.Parameter
	}
	metadata := map[string]any{MetaRule: f.Code}
	message := f.Detail
	if location != "" {
		metadata[MetaField] = location
		message = location + ": " + f.Detail
	}
	return fault.Detail{Code: code, Message: message, Metadata: metadata}
}

// statusCode turns a status into an upper snake code: 404 is NOT_FOUND.
func statusCode(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return fault.ErrUnexpected.Details[0].Code
	}
	return strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_", "'", "").Replace(text))
}

// Write writes resp to w: the envelope as fault.WriteJSON writes it, a
// server-sent event frame for a stream, or the bare status when there is no
// body. A stream frame is not written when the response already started with
// another Content-Type; Write then reports false. Behind core/log's
// HTTPMiddleware, a frame after the stream started does not write the status
// again (see log.ResponseStatus); without it, the status is written as the
// v0.22.0 handler did.
func (resp Response) Write(w http.ResponseWriter) bool {
	switch {
	case resp.Stream && resp.Body == nil:
		return true
	case resp.Stream:
		return writeStream(w, resp.Status, resp.Body.Message())
	case resp.Body == nil:
		w.WriteHeader(resp.Status)
		return true
	}
	fault.WriteJSON(w, resp.Body)
	return true
}

func writeStream(w http.ResponseWriter, status int, message string) bool {
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/event-stream" && contentType != "" {
		return false
	}
	if contentType == "" {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
	}
	if _, written, known := log.ResponseStatus(w); !known || !written {
		w.WriteHeader(status)
	}
	// The payload is JSON-encoded, so a message with quotes or new lines
	// cannot break the frame.
	payload, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	if err != nil {
		payload = []byte(`{"error":"Unexpected error"}`)
	}
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}
