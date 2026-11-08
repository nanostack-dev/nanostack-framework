package nanoserver_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

// logBuffer collects JSON log lines from the server goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for line := range strings.Lines(b.buf.String()) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestRunCutsAStuckRequestAfterTheShutdownTimeoutAndSucceeds(t *testing.T) {
	entered, cut := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cut)
	})
	cfg := testConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond
	logs := &logBuffer{}
	srv := nanoserver.NewServer(cfg, handler, slog.New(slog.NewJSONHandler(logs, nil)))
	cancel, done := start(t, srv)

	go func() {
		resp, err := http.Get("http://" + srv.Addr().String() + "/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after a timed-out drain = %v, want nil: the stop is planned", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ShutdownTimeout")
	}
	select {
	case <-cut:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck request kept its connection after Run returned")
	}
	var warned []map[string]any
	for _, line := range logs.lines(t) {
		if line["level"] == "WARN" {
			warned = append(warned, line)
		}
	}
	if len(warned) != 1 {
		t.Fatalf("Warn lines = %v, want one line for the cut connections", warned)
	}
	if got := warned[0]["connections"]; got != float64(1) {
		t.Fatalf("connections = %v, want 1", got)
	}
}

func TestDrainingEndsAStreamBeforeTheShutdownTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hello\n\n")
		_ = http.NewResponseController(w).Flush()
		select {
		case <-nanoserver.Draining(r.Context()):
			_, _ = io.WriteString(w, "event: close\ndata: bye\n\n")
		case <-r.Context().Done():
		}
	})
	cfg := testConfig()
	cfg.ShutdownTimeout = 30 * time.Second
	srv := nanoserver.NewServer(cfg, handler, discard())
	cancel, done := start(t, srv)

	resp, err := http.Get("http://" + srv.Addr().String() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if line, readErr := reader.ReadString('\n'); readErr != nil || line != "data: hello\n" {
		t.Fatalf("first event = %q, %v", line, readErr)
	}
	began := time.Now()
	cancel()

	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("stream after the drain started: %v", err)
	}
	if !strings.Contains(string(rest), "event: close") {
		t.Fatalf("stream tail = %q, want the close event", rest)
	}
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run = %v, want nil", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run waited for the shutdown timeout although the stream ended")
	}
	if waited := time.Since(began); waited > 5*time.Second {
		t.Fatalf("drain took %s, want the stream to end at the drain signal", waited)
	}
}

func TestDrainingIsNilOutsideAServer(t *testing.T) {
	if ch := nanoserver.Draining(context.Background()); ch != nil {
		t.Fatalf("Draining outside a server = %v, want nil", ch)
	}
}
