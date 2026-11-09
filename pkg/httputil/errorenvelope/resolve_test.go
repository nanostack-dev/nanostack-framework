package errorenvelope_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	corefault "github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/validate"
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// webhookNotFound stands in for a generated catalogue error.
type webhookNotFound struct{ detail string }

func (e webhookNotFound) Error() string { return "webhook not found in shard 7" }
func (webhookNotFound) ProblemType() string {
	return corefault.DefaultTypeBase + "/webhooks/webhook_not_found"
}
func (webhookNotFound) ProblemStatus() int      { return http.StatusNotFound }
func (webhookNotFound) ProblemTitle() string    { return "Webhook not found" }
func (webhookNotFound) ProblemCode() string     { return "webhook_not_found" }
func (e webhookNotFound) ProblemDetail() string { return e.detail }

// legacyError is a service's own error type that an adapter converts.
type legacyError struct{}

func (legacyError) Error() string { return "legacy error" }

// body is a request body type whose validator reports what the test asks.
type body struct {
	Name string `json:"name"`
	errs validate.Errors
}

func (b *body) Validate(validate.Path) validate.Errors { return b.errs }

// golden is what fault.WriteJSON writes for apiErr.
func golden(t *testing.T, apiErr *fault.Error) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	fault.WriteJSON(rec, apiErr)
	return rec.Code, rec.Body.String()
}

// write writes the resolution of err with the response or the request hook.
func write(resp errorenvelope.Response) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	resp.Write(rec)
	return rec
}

func checkEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if rec.Code != status || rec.Body.String() != body {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

// TestResolveWritesTheBytesOfWriteJSON checks every handler-error row of
// design C3 against the bytes that fault.WriteJSON writes today.
func TestResolveWritesTheBytesOfWriteJSON(t *testing.T) {
	t.Parallel()
	conflict := fault.NewWithStatus("CONFLICT", "conflict", http.StatusConflict)
	adapted := fault.NotFound("LEGACY_NOT_FOUND", "legacy resource not found")
	opts := errorenvelope.Options{Adapter: func(err error) (*fault.Error, bool) {
		if errors.As(err, new(legacyError)) {
			return adapted, true
		}
		return nil, false
	}}
	cursor := &fault.Error{Status: http.StatusBadRequest, Details: []fault.Detail{{
		Code: "INVALID_CURSOR", Field: "pagination.cursor",
		Message: "The cursor is invalid for this search. Restart without a cursor, " +
			"or send back the next_cursor of the previous page with the same sort.",
	}}}
	tests := []struct {
		name       string
		err        error
		want       *fault.Error
		unmodelled bool
	}{
		{"pkg/fault error", conflict, conflict, false},
		{"wrapped pkg/fault error", fmt.Errorf("load: %w", fault.Unauthorized("UNAUTHORIZED", "no token")),
			fault.Unauthorized("UNAUTHORIZED", "no token"), false},
		{"fault with metadata", fault.Unprocessable("INVALID", "bad").Meta("limit", 100),
			fault.Unprocessable("INVALID", "bad").Meta("limit", 100), false},
		{"adapter", fmt.Errorf("wrapped: %w", legacyError{}), adapted, false},
		{"invalid cursor", fmt.Errorf("page: %w", search.ErrInvalidCursor), cursor, false},
		{"cursor with offset", search.ErrCursorWithOffset, &fault.Error{Status: http.StatusBadRequest,
			Details: []fault.Detail{{Code: "INVALID_CURSOR", Field: "pagination.cursor",
				Message: "A cursor cannot be combined with an offset."}}}, false},
		{"unknown sort", search.ErrUnknownSort, &fault.Error{Status: http.StatusBadRequest,
			Details: []fault.Detail{{Code: "INVALID_SORT", Field: "sort_by",
				Message: "The search cannot sort by this field."}}}, false},
		{"invalid sort direction", fmt.Errorf("%w: %q", dbsearch.ErrInvalidSortDirection, "desc"),
			&fault.Error{Status: http.StatusBadRequest, Details: []fault.Detail{{Code: "INVALID_SORT",
				Field: "sort_direction", Message: "The sort direction must be ASC or DESC."}}}, false},
		{"db/search invalid cursor", dbsearch.ErrInvalidCursor, cursor, false},
		{
			"invalid limit",
			fmt.Errorf("page: %w", fmt.Errorf("%w: 0 is not between 1 and 1000", dbsearch.ErrInvalidLimit)),
			&fault.Error{Status: http.StatusBadRequest, Details: []fault.Detail{{Code: "BAD_REQUEST",
				Field: "pagination.limit", Message: "The page size must be between 1 and 1000."}}},
			false,
		},
		{"invalid offset", dbsearch.ErrInvalidOffset, &fault.Error{Status: http.StatusBadRequest,
			Details: []fault.Detail{{Code: "BAD_REQUEST", Field: "pagination.offset",
				Message: "The offset cannot be negative."}}}, false},
		{"too many values", fmt.Errorf("filter ids: %w", dbsearch.ErrTooManyValues), &fault.Error{
			Status: http.StatusBadRequest, Details: []fault.Detail{{Code: "BAD_REQUEST",
				Message: "A filter has more than 1000 values."}}}, false},
		{"term too long", dbsearch.ErrTermTooLong, &fault.Error{Status: http.StatusBadRequest,
			Details: []fault.Detail{{Code: "BAD_REQUEST",
				Message: "A search term is longer than 1000 characters."}}}, false},
		{"unscoped", fmt.Errorf("search webhooks: %w", dbsearch.ErrUnscoped), fault.ErrUnexpected, true},
		{
			"unscoped joined with a client error",
			errors.Join(dbsearch.ErrTooManyValues, dbsearch.ErrUnscoped),
			fault.ErrUnexpected,
			true,
		},
		{"catalogue error with detail", fmt.Errorf("get: %w", webhookNotFound{detail: "No webhook has id wh_2."}),
			fault.NotFound("WEBHOOK_NOT_FOUND", "No webhook has id wh_2."), false},
		{"catalogue error without detail", webhookNotFound{},
			fault.NotFound("WEBHOOK_NOT_FOUND", "Webhook not found"), false},
		{"catalogue kind", corefault.NewKind("invoice_already_paid", http.StatusConflict, "Invoice already paid",
			corefault.DefaultTypeBase+"/billing/invoice_already_paid"),
			fault.Conflict("INVOICE_ALREADY_PAID", "Invoice already paid"), false},
		{"invalid described", corefault.NewKind("odd", http.StatusOK, "Odd", "x"), fault.ErrUnexpected, true},
		{"plain error", errors.New("pq: connection refused"), fault.ErrUnexpected, true},
		{"deadline exceeded", fmt.Errorf("query: %w", context.DeadlineExceeded), fault.ErrUnexpected, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := errorenvelope.Resolve(httptest.NewRequest(http.MethodGet, "/flows", nil), tt.err, opts)
			if resp.Unmodelled != tt.unmodelled || resp.Abandoned || resp.Stream {
				t.Fatalf("Response = %+v, want unmodelled %v", resp, tt.unmodelled)
			}
			status, want := golden(t, tt.want)
			checkEnvelope(t, write(resp), status, want)
		})
	}
}

func TestResolveKeepsTheCauseForLogs(t *testing.T) {
	t.Parallel()
	cause := webhookNotFound{}
	resp := errorenvelope.Resolve(httptest.NewRequest(http.MethodGet, "/", nil), cause, errorenvelope.Options{})
	if !errors.Is(resp.Body, cause) {
		t.Fatalf("Body %v does not wrap the catalogue error", resp.Body)
	}
}

func TestResolveAnswersAnAbandonedRequestWith499AndNoBody(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelled := httptest.NewRequest(http.MethodGet, "/flows", nil).WithContext(ctx)
	tests := []struct {
		name string
		r    *http.Request
		err  error
	}{
		{"error wraps the cancel", httptest.NewRequest(http.MethodGet, "/flows", nil),
			fmt.Errorf("query: %w", context.Canceled)},
		{"request context cancelled", cancelled, errors.New("driver: bad connection")},
		{"cancel wins over a fault wrapper", cancelled, fault.Internal("DB", "db failed").Wrap(context.Canceled)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := errorenvelope.Resolve(tt.r, tt.err, errorenvelope.Options{})
			rec := write(resp)
			if !resp.Abandoned || rec.Code != errorenvelope.StatusClientClosedRequest || rec.Body.Len() != 0 ||
				rec.Header().Get("Content-Type") != "" {
				t.Fatalf("got %d %q %q, want 499 with no body and no Content-Type",
					rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
			}
		})
	}
}

// decodeErr returns the error that nanohttp.Decode gives for a request.
func decodeErr(t *testing.T, contentType, payload string, errs validate.Errors) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(payload))
	if payload == "" {
		r.Body = http.NoBody
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	err := nanohttp.Decode(r, &body{errs: errs})
	if err == nil {
		t.Fatal("Decode returned no error")
	}
	return err
}

// TestResolveRequestMapsBoundaryErrors checks the boundary rows of design C3
// with the errors that the nano HTTP stack really produces.
func TestResolveRequestMapsBoundaryErrors(t *testing.T) {
	t.Parallel()
	_, parseErr := strconv.ParseInt("abc", 10, 64)
	minLength := validate.Errors{{Pointer: "/name", Code: validate.CodeMinLength,
		Detail: "must be at least 1 character long"}}
	tests := []struct {
		name   string
		err    error
		opts   errorenvelope.Options
		status int
		body   string
	}{
		{
			"malformed JSON",
			decodeErr(t, "application/json", "{", nil),
			errorenvelope.Options{},
			400,
			`{"errors":[{"code":"BAD_REQUEST","message":"is not valid JSON at byte offset 1","metadata":{"rule":"syntax"}}]}`,
		},
		{"body validation", decodeErr(t, "application/json", `{"name":""}`, minLength), errorenvelope.Options{}, 400,
			`{"errors":[{"code":"BAD_REQUEST","message":"/name: must be at least 1 character long",` +
				`"metadata":{"field":"/name","rule":"minLength"}}]}`},
		{"two violations", problem.Validation(append(minLength, validate.Error{Pointer: "/url",
			Code: validate.CodeRequired, Detail: "is required"})), errorenvelope.Options{}, 400,
			`{"errors":[{"code":"BAD_REQUEST","message":"/name: must be at least 1 character long",` +
				`"metadata":{"field":"/name","rule":"minLength"}},{"code":"BAD_REQUEST","message":"/url: is required",` +
				`"metadata":{"field":"/url","rule":"required"}}]}`},
		{"wrong type", decodeErr(t, "application/json", `{"name":1}`, nil), errorenvelope.Options{}, 400,
			`{"errors":[{"code":"BAD_REQUEST","message":"/name: must be a string, got number",` +
				`"metadata":{"field":"/name","rule":"type"}}]}`},
		{"missing body", decodeErr(t, "application/json", "", nil), errorenvelope.Options{}, 400,
			`{"errors":[{"code":"BAD_REQUEST","message":"The request requires a JSON body."}]}`},
		{
			"unsupported media type",
			decodeErr(t, "text/plain", "{}", nil),
			errorenvelope.Options{},
			415,
			`{"errors":[{"code":"UNSUPPORTED_MEDIA_TYPE","message":"The request body must be application/json, not \"text/plain\"."}]}`,
		},
		{
			"body too large",
			fmt.Errorf("nanohttp: read body: %w", problem.BodyTooLarge(10)),
			errorenvelope.Options{},
			413,
			`{"errors":[{"code":"PAYLOAD_TOO_LARGE","message":"The request body exceeds 10 bytes."}]}`,
		},
		{
			"invalid parameter",
			problem.InvalidParameter("limit", parseErr),
			errorenvelope.Options{},
			400,
			`{"errors":[{"code":"BAD_REQUEST","message":"limit: is invalid","metadata":{"field":"limit","rule":"type"}}]}`,
		},
		{
			"missing parameter",
			problem.MissingParameter("from", errors.New(`query parameter "from" is required`)),
			errorenvelope.Options{},
			400,
			`{"errors":[{"code":"BAD_REQUEST","message":"from: is required","metadata":{"field":"from","rule":"required"}}]}`,
		},
		{
			"parameter validation",
			problem.InvalidParameters(validate.Errors{{Pointer: "/limit",
				Code: validate.CodeMaximum, Detail: "must be at most 100"}}),
			errorenvelope.Options{},
			400,
			`{"errors":[{"code":"BAD_REQUEST","message":"limit: must be at most 100","metadata":{"field":"limit","rule":"maximum"}}]}`,
		},
		{"boundary code option", problem.MissingBody(), errorenvelope.Options{BoundaryCode: "VALIDATION_ERROR"}, 400,
			`{"errors":[{"code":"VALIDATION_ERROR","message":"The request requires a JSON body."}]}`},
		{
			"error of the chi strict server",
			errors.New("can't decode JSON body: unexpected EOF"),
			errorenvelope.Options{},
			400,
			`{"errors":[{"code":"BAD_REQUEST","message":"can't decode JSON body: unexpected EOF"}]}`,
		},
		{"nil error", nil, errorenvelope.Options{}, 400,
			`{"errors":[{"code":"BAD_REQUEST","message":"Bad Request"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := errorenvelope.ResolveRequest(
				httptest.NewRequest(http.MethodPost, "/webhooks", nil),
				tt.err,
				tt.opts,
			)
			checkEnvelope(t, write(resp), tt.status, tt.body+"\n")
		})
	}
}

// A hostile body produces at most validate.MaxErrors details: the envelope
// lists the violations the problem lists, which are bounded.
func TestResolveRequestBoundsTheDetails(t *testing.T) {
	t.Parallel()
	var errs validate.Errors
	for i := range 10 * validate.MaxErrors {
		errs.Add(validate.Root.Field("headers").Index(i).Field("name"), validate.CodeRequired, "is required")
	}
	rec := write(errorenvelope.ResolveRequest(
		httptest.NewRequest(http.MethodPost, "/webhooks", nil),
		decodeErr(t, "application/json", `{"headers":[]}`, errs),
		errorenvelope.Options{},
	))
	if n := strings.Count(rec.Body.String(), `"code":"BAD_REQUEST"`); n != validate.MaxErrors {
		t.Fatalf("the envelope lists %d details, want %d", n, validate.MaxErrors)
	}
	if rec.Body.Len() > 32<<10 {
		t.Fatalf("envelope of %d bytes", rec.Body.Len())
	}
}

// A handler that returns a boundary problem, such as problem.NotFound, gets
// the boundary mapping too, not the generic Described one.
func TestResolveMapsABoundaryProblemFromAHandler(t *testing.T) {
	t.Parallel()
	resp := errorenvelope.Resolve(
		httptest.NewRequest(http.MethodGet, "/", nil),
		problem.NotFound(),
		errorenvelope.Options{},
	)
	checkEnvelope(
		t,
		write(resp),
		404,
		`{"errors":[{"code":"NOT_FOUND","message":"No route matches the request path."}]}`+"\n",
	)
}

func TestResolveWritesAStreamFrame(t *testing.T) {
	t.Parallel()
	stream := errorenvelope.Options{IsStream: func(*http.Request) bool { return true }}
	r := httptest.NewRequest(http.MethodGet, "/events", nil)

	resp := errorenvelope.Resolve(
		r,
		fault.NotFound("NOT_FOUND", `flow "x" not found`).Wrap(errors.New("secret")),
		stream,
	)
	rec := write(resp)
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "text/event-stream" ||
		rec.Body.String() != "event: error\ndata: {\"error\":\"flow \\\"x\\\" not found\"}\n\n" {
		t.Fatalf("got %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	resp = errorenvelope.Resolve(r, errors.New("boom"), stream)
	if rec = write(
		resp,
	); rec.Code != 500 || !strings.Contains(rec.Body.String(), "Unexpected error") ||
		!resp.Unmodelled {
		t.Fatalf("got %d %q, want the generic 500 frame", rec.Code, rec.Body.String())
	}

	for _, err := range []error{context.Canceled, fmt.Errorf("x: %w", context.DeadlineExceeded)} {
		resp = errorenvelope.Resolve(r, err, stream)
		if rec = write(resp); !resp.Abandoned || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
			t.Fatalf("%v: got %+v %q, want nothing written", err, resp, rec.Body.String())
		}
	}

	started := httptest.NewRecorder()
	started.Header().Set("Content-Type", "application/json")
	if resp = errorenvelope.Resolve(r, errors.New("boom"), stream); resp.Write(started) {
		t.Fatal("Write wrote a frame into a response that started as application/json")
	}
}
