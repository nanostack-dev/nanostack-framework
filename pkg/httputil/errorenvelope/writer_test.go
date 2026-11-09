package errorenvelope_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/validate"
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// lines decodes each JSON log line in buf.
func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// failedLine returns the one "request failed" line.
func failedLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, m := range lines(t, buf) {
		if m["message"] == "request failed" || m["msg"] == "request failed" {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		t.Fatalf("request failed lines = %v, want one in %s", found, buf.String())
	}
	return found[0]
}

func newLogger(buf *bytes.Buffer, wire log.Wire) *slog.Logger {
	return log.New(log.Config{Output: buf, Level: slog.LevelDebug, Wire: wire})
}

// TestWriterLogsTheCauseWithTheRequestScope is the slog cause line that a
// service on core/log needs: under log.HTTPMiddleware, the line of a 500
// carries the request_id that the client got, the route, and the cause.
func TestWriterLogsTheCauseWithTheRequestScope(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := newLogger(&buf, log.WireLegacy)
	envelope := errorenvelope.New(errorenvelope.Options{Logger: logger})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks", func(w http.ResponseWriter, r *http.Request) {
		envelope.ResponseError(w, r, errors.New("pq: connection refused"))
	})
	handler := log.HTTPMiddleware(log.HTTPOptions{Logger: logger, LegacySummary: true})(mux)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", nil)
	req.Header.Set(log.RequestIDHeader, "rid-7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	checkEnvelope(t, rec, 500, `{"errors":[{"code":"UNEXPECTED_ERROR","message":"Unexpected error"}]}`+"\n")
	line := failedLine(t, &buf)
	want := map[string]any{
		"level": "error", log.KeyRequestID: "rid-7", log.KeyStatus: float64(500), log.KeyRoute: "POST /webhooks",
		log.KeyError: "pq: connection refused", errorenvelope.KeyErrorType: "*errors.errorString",
		log.KeyMethod: "POST", log.KeyPath: "/webhooks",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v (line %v)", k, line[k], v, line)
		}
	}
}

func TestWriterLogLevels(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name  string
		r     *http.Request
		err   error
		level string
	}{
		{
			"modelled client error",
			httptest.NewRequest(http.MethodGet, "/", nil),
			fault.NotFound("NOT_FOUND", "no"),
			"DEBUG",
		},
		{"modelled server error", httptest.NewRequest(http.MethodGet, "/", nil), fault.Internal("DB", "db"), "ERROR"},
		{"abandoned request", httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
			errors.New("driver: bad connection"), "WARN"},
		{"search page out of range", httptest.NewRequest(http.MethodPost, "/", nil),
			fmt.Errorf("search: %w", dbsearch.ErrInvalidLimit), "DEBUG"},
		{"search with no tenant scope", httptest.NewRequest(http.MethodPost, "/", nil),
			fmt.Errorf("search: %w", dbsearch.ErrUnscoped), "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			errorenvelope.New(errorenvelope.Options{Logger: newLogger(&buf, log.WireSlog)}).
				ResponseError(httptest.NewRecorder(), tt.r, tt.err)
			if line := failedLine(t, &buf); line["level"] != tt.level {
				t.Fatalf("level = %v, want %s", line["level"], tt.level)
			}
		})
	}
}

func TestWriterRequestError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	envelope := errorenvelope.New(errorenvelope.Options{Logger: newLogger(&buf, log.WireSlog)})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader("{"))
	r.Header.Set("Content-Type", "application/json")
	envelope.RequestError(rec, r, nanohttp.Decode(r, &body{}))
	checkEnvelope(
		t,
		rec,
		400,
		`{"errors":[{"code":"BAD_REQUEST","message":"is not valid JSON at byte offset 1","metadata":{"rule":"syntax"}}]}`+"\n",
	)
	if line := failedLine(t, &buf); line["level"] != "DEBUG" || line[log.KeyStatus] != float64(400) {
		t.Fatalf("line = %v, want a Debug line with status 400", line)
	}
}

func TestWriterStreamClosedByClientWritesNothing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	envelope := errorenvelope.New(errorenvelope.Options{
		Logger:   newLogger(&buf, log.WireSlog),
		IsStream: func(*http.Request) bool { return true },
	})
	rec := httptest.NewRecorder()
	envelope.ResponseError(rec, httptest.NewRequest(http.MethodGet, "/events", nil), context.Canceled)
	if rec.Body.Len() != 0 || strings.Contains(buf.String(), "request failed") {
		t.Fatalf("body %q, log %s: want nothing written and no failure line", rec.Body.String(), buf.String())
	}
}

// TestWriterRouterHandlers plugs the Writer into nanohttp.NewRouter: the
// default 404 and 405 envelopes, and a service's own answer through Handler.
func TestWriterRouterHandlers(t *testing.T) {
	t.Parallel()
	envelope := errorenvelope.New(errorenvelope.Options{Logger: slog.New(slog.DiscardHandler)})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(http.ResponseWriter, *http.Request) {})

	router := nanohttp.NewRouter(mux, nanohttp.RouterOptions{
		NotFound: envelope.NotFound(), MethodNotAllowed: envelope.MethodNotAllowed(), ExplicitHEAD: true,
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nothing", nil))
	checkEnvelope(t, rec, 404, `{"errors":[{"code":"NOT_FOUND","message":"No route matches the request path."}]}`+"\n")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/health", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("HEAD /health = %d Allow %q %q", rec.Code, rec.Header().Get("Allow"), rec.Header().Get("Content-Type"))
	}

	// A mux is wrapped once: the second router gets its own.
	mux = http.NewServeMux()
	mux.HandleFunc("GET /health", func(http.ResponseWriter, *http.Request) {})
	router = nanohttp.NewRouter(mux, nanohttp.RouterOptions{
		NotFound: envelope.Handler(fault.BadRequest("BAD_REQUEST", "no matching operation was found")),
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/definitely-not-a-real-route", nil))
	checkEnvelope(t, rec, 400, `{"errors":[{"code":"BAD_REQUEST","message":"no matching operation was found"}]}`+"\n")
}

func TestPlainRequestError(t *testing.T) {
	t.Parallel()
	_, parseErr := strconv.ParseInt("abc", 10, 64)
	binding := fmt.Errorf("Invalid format for parameter limit: %w", parseErr)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"binding error of a parameter problem", problem.InvalidParameter("limit", binding), binding.Error()},
		{"validation problem", problem.InvalidParameters(validate.Errors{{Pointer: "/limit",
			Code: validate.CodeMaximum, Detail: "must be at most 100"}}), "limit: must be at most 100"},
		{"problem without fields", problem.MissingBody(), "The request requires a JSON body."},
		{"plain error, first line", errors.New("parameter \"from\" in query is required\nmore"),
			`parameter "from" in query is required`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			errorenvelope.PlainRequestError(rec, httptest.NewRequest(http.MethodGet, "/", nil), tt.err)
			if rec.Code != 400 || rec.Body.String() != tt.want+"\n" ||
				rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("got %d %q %q, want 400 text/plain %q",
					rec.Code, rec.Header().Get("Content-Type"), rec.Body.String(), tt.want)
			}
		})
	}
}
