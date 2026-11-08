package errorenvelope_test

import (
	"bytes"
	"errors"
	"io"
	stdlog "log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// syncBuffer is a bytes.Buffer for the server goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A stream that already sent 200 and frames then fails: the error frame is
// appended, the status is not written a second time (net/http would log a
// superfluous WriteHeader), and the failure line reports the 200 the client
// received.
func TestStreamErrorAfterTheStreamStartedKeepsTheSentStatus(t *testing.T) {
	var logs bytes.Buffer
	envelope := errorenvelope.New(errorenvelope.Options{
		Logger:   newLogger(&logs, log.WireSlog),
		IsStream: func(*http.Request) bool { return true },
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: 1\n\n")
		_ = http.NewResponseController(w).Flush()
		envelope.ResponseError(w, r, errors.New("upstream broke"))
	})
	var serverLog syncBuffer
	srv := httptest.NewUnstartedServer(
		log.HTTPMiddleware(log.HTTPOptions{Logger: slog.New(slog.DiscardHandler)})(handler),
	)
	srv.Config.ErrorLog = stdlog.New(&serverLog, "", 0)
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	srv.Close() // flushes the server's error log

	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "event: error\n") {
		t.Fatalf("response = %d %q, want 200 with the error frame", resp.StatusCode, body)
	}
	if got := serverLog.String(); strings.Contains(got, "superfluous") {
		t.Fatalf("server log: %s", got)
	}
	line := failedLine(t, &logs)
	if line["status"] != float64(http.StatusOK) || line["level"] != "ERROR" {
		t.Fatalf("failure line = %v, want status 200 (sent) at ERROR", line)
	}
}

// Before the stream started, the error keeps its own status.
func TestStreamErrorBeforeTheStreamStartedSendsItsStatus(t *testing.T) {
	var logs bytes.Buffer
	envelope := errorenvelope.New(errorenvelope.Options{
		Logger:   newLogger(&logs, log.WireSlog),
		IsStream: func(*http.Request) bool { return true },
	})
	handler := log.HTTPMiddleware(log.HTTPOptions{Logger: slog.New(slog.DiscardHandler)})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			envelope.ResponseError(w, r, errors.New("upstream broke"))
		}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))

	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %d %q, want 500 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
	}
	if line := failedLine(t, &logs); line["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("failure line = %v, want status 500", line)
	}
}

// A response that started with another Content-Type gets no frame, and the
// log says why.
func TestStreamErrorAfterAnotherContentTypeExplainsTheMissingFrame(t *testing.T) {
	var logs bytes.Buffer
	envelope := errorenvelope.New(errorenvelope.Options{
		Logger:   newLogger(&logs, log.WireSlog),
		IsStream: func(*http.Request) bool { return true },
	})
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	envelope.ResponseError(rec, httptest.NewRequest(http.MethodGet, "/events", nil), errors.New("boom"))

	if strings.Contains(rec.Body.String(), "event: error") {
		t.Fatalf("body = %q, want no frame", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "started with another content type") ||
		!strings.Contains(logs.String(), `"content_type":"application/json"`) {
		t.Fatalf("log = %s, want the reason the frame is not sent", logs.String())
	}
}
