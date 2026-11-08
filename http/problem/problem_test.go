package problem_test

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// catalogueError stands in for a generated error catalogue entry.
type catalogueError struct{ cause error }

func (e catalogueError) Error() string { return "invoice already paid: " + e.cause.Error() }
func (e catalogueError) Unwrap() error { return e.cause }
func (catalogueError) ProblemType() string {
	return "https://errors.nanostack.dev/billing/invoice_already_paid"
}
func (catalogueError) ProblemStatus() int    { return http.StatusConflict }
func (catalogueError) ProblemTitle() string  { return "Invoice is already paid" }
func (catalogueError) ProblemDetail() string { return "Invoice 7f was paid." }
func (catalogueError) ProblemCode() string   { return "billing.invoice_already_paid" }

type bareDescribed struct{}

func (bareDescribed) Error() string        { return "secret internals" }
func (bareDescribed) ProblemType() string  { return "https://errors.nanostack.dev/x/y" }
func (bareDescribed) ProblemStatus() int   { return http.StatusNotFound }
func (bareDescribed) ProblemTitle() string { return "" }

type badStatus struct{ bareDescribed }

func (badStatus) ProblemStatus() int { return 200 }

type untyped struct{ bareDescribed }

func (untyped) ProblemType() string { return "" }

// TestInvalidDescribedIsA500EverywhereAlike checks that the problem writer
// and log.LevelFor apply one rule, fault.Valid: a Described with an empty
// type or a status outside 400 to 599 renders as a 500 and logs at Error,
// whether through Write or through log.Error.
func TestInvalidDescribedIsA500EverywhereAlike(t *testing.T) {
	for _, err := range []error{untyped{}, badStatus{}} {
		buf := captureLogs(t)
		got, rec := write(t, err)
		lines := logLines(t, buf)
		if rec.Code != http.StatusInternalServerError || got.Type != problem.TypeInternal {
			t.Errorf("%T: problem = %d %s, want a 500", err, rec.Code, got.Type)
		}
		if len(lines) != 1 || lines[0]["level"] != "ERROR" {
			t.Errorf("%T: Write logged %s, want one ERROR line", err, buf.String())
		}
		if level := log.LevelFor(err); level != slog.LevelError {
			t.Errorf("%T: LevelFor = %v, want ERROR as the response is a 500", err, level)
		}
	}
}

func TestWriteDescribed(t *testing.T) {
	err := fmt.Errorf("pay: %w", catalogueError{cause: errors.New("pq: tx aborted at 10.0.0.3")})
	got, rec := write(t, err)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q", ct)
	}
	want := problem.Problem{
		Type: "https://errors.nanostack.dev/billing/invoice_already_paid", Title: "Invoice is already paid",
		Status: 409, Detail: "Invoice 7f was paid.", Instance: "/invoices/7f/pay",
		Code: "billing.invoice_already_paid", TraceID: "0102030405060708090a0b0c0d0e0f10",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("problem = %+v\nwant      %+v", got, want)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.3") {
		t.Fatal("the cause leaked")
	}
}

func TestDescribedDefaults(t *testing.T) {
	p := problem.FromError(bareDescribed{})
	if p.Title != "Not Found" || p.Detail != "" || p.Status != 404 {
		t.Fatalf("problem = %+v", p)
	}
	if fallback := problem.FromError(badStatus{}); fallback.Status != 500 || fallback.Type != problem.TypeInternal {
		t.Fatalf("a non-error status must fall back to 500: %+v", fallback)
	}
}

func TestInternalFallbackLeaksNothing(t *testing.T) {
	for _, err := range []error{
		errors.New("dial tcp 10.0.0.3:5432: connection refused"),
		nil,
	} {
		got, rec := write(t, err)
		if rec.Code != 500 || got.Type != problem.TypeInternal || got.Detail != "" || got.Errors != nil {
			t.Fatalf("problem = %+v", got)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.3") {
			t.Fatal("internal error text leaked")
		}
	}
}

func TestContextErrors(t *testing.T) {
	if p := problem.FromError(fmt.Errorf("q: %w", context.DeadlineExceeded)); p.Status != 504 {
		t.Fatalf("deadline = %+v", p)
	}
	if p := problem.FromError(context.Canceled); p.Status != 499 {
		t.Fatalf("canceled = %+v", p)
	}
	if p := problem.FromError(&http.MaxBytesError{Limit: 10}); p.Status != 413 {
		t.Fatalf("max bytes = %+v", p)
	}
}

func TestValidationBody(t *testing.T) {
	var errs validate.Errors
	errs.Add(validate.Root, validate.CodeType, "must be an object")
	errs.Add(
		validate.Root.Field("headers").Index(2).Field("name"),
		validate.CodeMaxLength,
		"must be at most 255 characters",
	)
	_, rec := write(t, problem.Validation(errs))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"errors":[{"pointer":"","code":"type","detail":"must be an object"},`,
		`{"pointer":"/headers/2/name","code":"maxLength","detail":"must be at most 255 characters"}]`,
		`"code":"http.validation_failed"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s\nmissing %s", body, want)
		}
	}
}

func TestInvalidParameters(t *testing.T) {
	var errs validate.Errors
	errs.Add(validate.Root.Field("limit"), validate.CodeMaximum, "must be at most 100")
	got, rec := write(t, problem.InvalidParameters(errs))
	if rec.Code != 400 || len(got.Errors) != 1 || got.Errors[0].Parameter != "limit" {
		t.Fatalf("problem = %+v", got)
	}
	if !strings.Contains(rec.Body.String(), `{"parameter":"limit","code":"maximum","detail":"must be at most 100"}`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	got, _ = write(t, problem.InvalidParameter("id", errors.New("strconv: bad")))
	if got.Errors[0] != (problem.FieldError{Parameter: "id", Code: "type", Detail: "is invalid"}) {
		t.Fatalf("problem = %+v", got)
	}
}

func TestInvalidBodyAndMalformed(t *testing.T) {
	var v struct {
		Count int `json:"count"`
	}
	err := jsonv2.Unmarshal([]byte(`{"count":"x"}`), &v, validate.DecodeOptions())
	got, _ := write(t, problem.InvalidBody("/count", err))
	if got.Status != 400 ||
		got.Errors[0] != (problem.FieldError{Pointer: "/count", Code: "type", Detail: "must be an integer, got string"}) {
		t.Fatalf("invalid body = %+v", got)
	}

	cases := map[string]problem.FieldError{
		`{"a":1,"a":2}`:    {Pointer: "/a", Code: validate.CodeDuplicateName, Detail: "repeats an object member name"},
		"{\"a\":\"\xff\"}": {Pointer: "/a", Code: validate.CodeInvalidUTF8, Detail: "contains invalid UTF-8"},
		`{"a":1} {}`:       {Code: validate.CodeTrailingData, Detail: "has data after the JSON value"},
		`{"a":}`:           {Pointer: "/a", Code: validate.CodeSyntax},
	}
	for in, want := range cases {
		syntaxErr := jsonv2.Unmarshal([]byte(in), &v, validate.DecodeOptions())
		malformed, _ := write(t, problem.MalformedJSON(syntaxErr))
		if want.Code == validate.CodeSyntax { // the detail names a byte offset
			want.Detail = malformed.Errors[0].Detail
		}
		if malformed.Status != 400 || malformed.Errors[0] != want {
			t.Errorf("%q: %+v, want %+v", in, malformed.Errors, want)
		}
	}
}

// captureLogs routes slog's default logger to a JSON buffer at debug level
// for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		if err := jsonv2.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		lines = append(lines, m)
	}
	return lines
}

// The request carries no logger: a plain slog service must still see the
// failure.
func TestLogsServerErrorsWithCause(t *testing.T) {
	buf := captureLogs(t)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	problem.Write(httptest.NewRecorder(), r, errors.New("db down"))

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("log = %s", buf.String())
	}
	got := lines[0]
	if got["level"] != "ERROR" || got["msg"] != "request failed" || got["error"] != "db down" ||
		got["problem"] != problem.TypeInternal || got["status"] != float64(500) {
		t.Fatalf("log = %v", got)
	}
}

func TestLogLevelFollowsTheResponse(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{bareDescribed{}, "DEBUG"},
		{problem.MissingBody(), "DEBUG"},
		{fmt.Errorf("query: %w", context.Canceled), "WARN"},
		{context.DeadlineExceeded, "WARN"},
		{errors.New("boom"), "ERROR"},
		// core/log decides the level, so an error that picks its own level
		// keeps it, and a 413 without a ProblemStatus method is still Debug.
		{selfLevelled{}, "INFO"},
		{&http.MaxBytesError{Limit: 8}, "DEBUG"},
	}
	for _, tc := range cases {
		buf := captureLogs(t)
		problem.Write(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
		lines := logLines(t, buf)
		if len(lines) != 1 || lines[0]["level"] != tc.want {
			t.Errorf("%v: log = %s, want level %s", tc.err, buf.String(), tc.want)
		}
	}
}

// TestCancelledRequestIsNotAServerError covers a handler that returns its
// driver's error after the client left: the error does not wrap
// context.Canceled, but the request context is done.
func TestCancelledRequestIsNotAServerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	timedOut, cancelTimeout := context.WithTimeout(context.Background(), 0)
	defer cancelTimeout()
	cases := []struct {
		ctx    context.Context
		err    error
		status int
		level  string
	}{
		{ctx, errors.New("driver: bad connection"), problem.StatusClientClosedRequest, "WARN"},
		{timedOut, errors.New("driver: bad connection"), http.StatusGatewayTimeout, "WARN"},
		// An error with its own status keeps it.
		{ctx, bareDescribed{}, http.StatusNotFound, "DEBUG"},
		{ctx, catalogueError{cause: errors.New("x")}, http.StatusConflict, "DEBUG"},
	}
	for _, tc := range cases {
		buf := captureLogs(t)
		rec := httptest.NewRecorder()
		problem.Write(rec, httptest.NewRequestWithContext(tc.ctx, http.MethodGet, "/x", nil), tc.err)
		lines := logLines(t, buf)
		if rec.Code != tc.status || len(lines) != 1 || lines[0]["level"] != tc.level {
			t.Errorf("%v: status %d, log %s; want %d at %s", tc.err, rec.Code, buf.String(), tc.status, tc.level)
			continue
		}
		if got, _ := lines[0][log.KeyError].(string); !strings.Contains(got, tc.err.Error()) {
			t.Errorf("%v: logged error %q drops the handler's error", tc.err, got)
		}
	}
}

// TestServerErrorGivesFailureAndAccessLines locks the decision in
// core/log/README.md: a 5xx through log.HTTPMiddleware gives one "request
// failed" line with the error and one summary line without it, both Error.
func TestServerErrorGivesFailureAndAccessLines(t *testing.T) {
	buf := captureLogs(t)
	h := log.HTTPMiddleware(log.HTTPOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, errors.New("boom"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	lines := logLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("log = %s, want two lines", buf.String())
	}
	failure, summary := lines[0], lines[1]
	if failure["msg"] != "request failed" || failure["level"] != "ERROR" || failure[log.KeyError] != "boom" {
		t.Errorf("failure line = %v", failure)
	}
	if summary["msg"] != "http request" || summary["level"] != "ERROR" || summary[log.KeyError] != nil {
		t.Errorf("summary line = %v", summary)
	}
}

// selfLevelled is an error that picks its own log level, as core/log's
// LevelFor allows.
type selfLevelled struct{}

func (selfLevelled) Error() string     { return "quota check skipped" }
func (selfLevelled) Level() slog.Level { return slog.LevelInfo }

// TestWriteLogsUnderCoreLogKeys checks that Write writes core/log's keys and
// the status it answered, also for an error that carries no status.
func TestWriteLogsUnderCoreLogKeys(t *testing.T) {
	buf := captureLogs(t)
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, &http.MaxBytesError{Limit: 8})
	})
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/flows/7", nil))
	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("log = %s", buf.String())
	}
	got := lines[0]
	if got["level"] != "DEBUG" || got[log.KeyError] != "http: request body too large" ||
		got[log.KeyStatus] != float64(http.StatusRequestEntityTooLarge) ||
		got[log.KeyProblem] != problem.TypeBodyTooLarge || got[log.KeyRoute] != "GET /flows/{id}" {
		t.Fatalf("log = %v", got)
	}
	// The source is Write itself: the handler that failed has returned
	// before a generated server calls Write.
	if src, ok := got[slog.SourceKey].(map[string]any); !ok ||
		src["function"] != "github.com/nanostack-dev/nanostack-framework/http/problem.Write" {
		t.Fatalf("source = %v, want problem.Write", got[slog.SourceKey])
	}
}

// A generated catalogue error is a fault.Kind or wraps one: problem renders
// it through package fault's interfaces, the single definition.
func TestRendersFaultCatalogueErrors(t *testing.T) {
	kind := fault.NewKind("billing.invoice_not_found", http.StatusNotFound, "Invoice not found",
		fault.DefaultTypeBase+"/billing/invoice_not_found")
	got, rec := write(t, fmt.Errorf("load: %w", kind))
	if rec.Code != http.StatusNotFound || got.Code != "billing.invoice_not_found" ||
		got.Type != "https://errors.nanostack.dev/billing/invoice_not_found" || got.Title != "Invoice not found" {
		t.Fatalf("problem = %+v", got)
	}
}

func TestFrameworkTypesUseTheFaultTypeBase(t *testing.T) {
	for _, typ := range []string{problem.TypeValidation, problem.TypeInternal, problem.TypeBadRequest} {
		if !strings.HasPrefix(typ, fault.DefaultTypeBase+"/http/") || strings.Contains(typ, "//http") {
			t.Errorf("type %q is not <fault.DefaultTypeBase>/http/<name>", typ)
		}
	}
}

func write(t *testing.T, err error) (problem.Problem, *httptest.ResponseRecorder) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/invoices/7f/pay", nil)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:  trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
	})
	r = r.WithContext(trace.ContextWithSpanContext(r.Context(), sc))
	rec := httptest.NewRecorder()
	problem.Write(rec, r, err)
	var p problem.Problem
	if decodeErr := jsonv2.Unmarshal(rec.Body.Bytes(), &p); decodeErr != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), decodeErr)
	}
	return p, rec
}

// TestOneFieldErrorConversion checks the single conversion from the domain
// shape (validate.Error) to the wire shape (FieldError) for a body and for
// parameters: Detail carries over unchanged, and a parameter pointer becomes
// the parameter name.
func TestOneFieldErrorConversion(t *testing.T) {
	v := validate.Error{Pointer: "/a~1b", Code: validate.CodeMaximum, Detail: "must be at most 100"}
	body, _ := write(t, problem.Validation(validate.Errors{v}))
	params, _ := write(t, problem.InvalidParameters(validate.Errors{v}))
	wantBody := problem.FieldError{Pointer: "/a~1b", Code: validate.CodeMaximum, Detail: "must be at most 100"}
	wantParam := problem.FieldError{Parameter: "a/b", Code: validate.CodeMaximum, Detail: "must be at most 100"}
	if len(body.Errors) != 1 || body.Errors[0] != wantBody {
		t.Errorf("body errors = %+v, want %+v", body.Errors, wantBody)
	}
	if len(params.Errors) != 1 || params.Errors[0] != wantParam {
		t.Errorf("parameter errors = %+v, want %+v", params.Errors, wantParam)
	}
}
