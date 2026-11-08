package errorenvelope_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// TestOptionsLogReplacesTheWriterLine checks that Options.Log receives every
// failure once, after the response is written, with its source, and that the
// Writer then writes no line of its own.
func TestOptionsLogReplacesTheWriterLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	var got []errorenvelope.Failure
	var codes []int
	envelope := errorenvelope.New(errorenvelope.Options{
		Logger: newLogger(&buf, log.WireSlog),
		Log: func(r *http.Request, f errorenvelope.Failure) {
			if r == nil {
				t.Error("Log got a nil request")
			}
			got = append(got, f)
		},
	})
	for _, call := range []func(http.ResponseWriter, *http.Request){
		func(w http.ResponseWriter, r *http.Request) { envelope.RequestError(w, r, errors.New("bad body")) },
		func(w http.ResponseWriter, r *http.Request) {
			envelope.ResponseError(w, r, fault.NotFound("NOT_FOUND", "no"))
		},
		envelope.NotFound().ServeHTTP,
	} {
		rec := httptest.NewRecorder()
		call(rec, httptest.NewRequest(http.MethodGet, "/flows", nil))
		codes = append(codes, rec.Code)
	}

	if buf.Len() != 0 {
		t.Fatalf("the Writer logged with Options.Log set: %s", buf.String())
	}
	want := []errorenvelope.Source{
		errorenvelope.SourceRequest, errorenvelope.SourceResponse, errorenvelope.SourceRouter,
	}
	if len(got) != len(want) {
		t.Fatalf("Log calls = %d, want %d", len(got), len(want))
	}
	for i, f := range got {
		if f.Source != want[i] || f.Err == nil || !f.Written ||
			f.SentStatus != f.Response.Status || codes[i] != f.SentStatus {
			t.Errorf("call %d: failure = %+v, response code %d", i, f, codes[i])
		}
	}
}

// TestOptionsLogSeesTheSentStatusAndTheMissingFrame covers a stream: the
// failure carries the status of the error and the one the client received,
// and whether the frame was written.
func TestOptionsLogSeesTheSentStatusAndTheMissingFrame(t *testing.T) {
	t.Parallel()
	var got errorenvelope.Failure
	envelope := errorenvelope.New(errorenvelope.Options{
		IsStream: func(*http.Request) bool { return true },
		Log:      func(_ *http.Request, f errorenvelope.Failure) { got = f },
	})
	handler := log.HTTPMiddleware(log.HTTPOptions{SkipSummary: func(*http.Request) bool { return true }})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			envelope.ResponseError(w, r, errors.New("upstream broke"))
		}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events", nil))

	if got.Response.Status != http.StatusInternalServerError || got.SentStatus != http.StatusAccepted || got.Written {
		t.Fatalf("failure = %+v, want status 500, sent 202, frame not written", got)
	}
}

// TestStrictErrorHandlerLogWritesTheLegacyLines pins the lines of
// modules/httpserver.StrictErrorHandler: message, level and attributes.
func TestStrictErrorHandlerLogWritesTheLegacyLines(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	get := func() *http.Request { return httptest.NewRequest(http.MethodGet, "/flows", nil) }
	tests := []struct {
		name     string
		stream   bool
		call     func(*errorenvelope.Writer, http.ResponseWriter)
		messages []string
		levels   []string
		status   float64
		detail   bool
	}{
		{
			name: "request hook", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.RequestError(w, get(), errors.New("bad body"))
			},
		},
		{
			name: "client error", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get(), fault.NotFound("NOT_FOUND", "no"))
			},
			messages: []string{"Request error"}, levels: []string{"info"}, status: 404,
		},
		{
			name: "router", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.MethodNotAllowed().ServeHTTP(w, get())
			},
			messages: []string{"Request error"}, levels: []string{"info"}, status: 405,
		},
		{
			name: "server error", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get(), fault.Internal("DB", "db"))
			},
			messages: []string{"Internal server error"}, levels: []string{"error"}, status: 500, detail: true,
		},
		{
			name: "unmodelled", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get(), errors.New("pq: connection refused"))
			},
			messages: []string{"Unhandled error returned by strict handler"}, levels: []string{"error"}, status: 500,
			detail: true,
		},
		{
			name: "abandoned", call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get().WithContext(canceled), errors.New("driver: bad connection"))
			},
			messages: []string{"Request abandoned by client"}, levels: []string{"warn"}, status: 499,
		},
		{
			name: "stream closed", stream: true, call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get(), context.Canceled)
			},
			messages: []string{"SSE stream closed by client"}, levels: []string{"debug"},
		},
		{
			name: "stream server error", stream: true, call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				e.ResponseError(w, get(), errors.New("upstream broke"))
			},
			messages: []string{"SSE stream error"}, levels: []string{"error"}, status: 500, detail: true,
		},
		{
			name: "stream client error after another content type", stream: true,
			call: func(e *errorenvelope.Writer, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				e.ResponseError(w, get(), fault.BadRequest("BAD_REQUEST", "no"))
			},
			messages: []string{"SSE stream warning", "SSE stream headers already written, cannot modify response"},
			levels:   []string{"info", "debug"}, status: 400,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			envelope := errorenvelope.New(errorenvelope.Options{
				IsStream: func(*http.Request) bool { return tt.stream },
				Log:      errorenvelope.StrictErrorHandlerLog(newLogger(&buf, log.WireLegacy)),
			})
			tt.call(envelope, httptest.NewRecorder())

			got := lines(t, &buf)
			if len(got) != len(tt.messages) {
				t.Fatalf("lines = %s, want %v", buf.String(), tt.messages)
			}
			for i, line := range got {
				if line["message"] != tt.messages[i] || line["level"] != tt.levels[i] {
					t.Errorf("line %d = %v, want %q at %s", i, line, tt.messages[i], tt.levels[i])
				}
				if caller, _ := line["caller"].(string); !strings.Contains(caller, "/errorenvelope/writer.go:") {
					t.Errorf("line %d caller = %q", i, caller)
				}
			}
			if tt.status == 0 {
				return
			}
			first := got[0]
			if first[log.KeyStatus] != tt.status || first[log.KeyError] == nil {
				t.Errorf("line = %v, want error and status %v", first, tt.status)
			}
			_, hasType := first[errorenvelope.KeyErrorType]
			_, hasDetail := first[errorenvelope.KeyErrorDetail]
			if hasType != tt.detail || hasDetail != tt.detail {
				t.Errorf("line = %v, error_type and error_detail present = %v, want %v", first, hasType, tt.detail)
			}
		})
	}
}
