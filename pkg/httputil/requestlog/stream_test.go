package requestlog_test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/requestlog"
	"github.com/rs/zerolog"
)

// A streaming handler behind the middleware must be able to clear the server's
// WriteTimeout through http.ResponseController; otherwise the stream dies once
// the timeout elapses.
func TestMiddlewareLetsStreamOutliveServerWriteTimeout(t *testing.T) {
	const (
		writeTimeout = 200 * time.Millisecond
		events       = 10
		interval     = 50 * time.Millisecond
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			t.Errorf("clear write deadline through the middleware: %v", err)
			return
		}
		for i := range events {
			if _, err := fmt.Fprintf(w, "event %d\n", i); err != nil {
				t.Errorf("write event %d: %v", i, err)
				return
			}
			if err := rc.Flush(); err != nil {
				t.Errorf("flush event %d: %v", i, err)
				return
			}
			time.Sleep(interval)
		}
	})

	server := httptest.NewUnstartedServer(requestlog.New(zerolog.Nop(), requestlog.Options{})(handler))
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	received := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		received++
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		t.Fatalf("read stream after %d events: %v", received, err)
	}
	if received != events {
		t.Fatalf("received %d events, want %d", received, events)
	}
}
