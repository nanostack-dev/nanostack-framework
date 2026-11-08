package nanoserver_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

// checker is the method set of pkg/health.Checker. This package may not
// import pkg/health, so the test pins the shape instead.
type checker interface {
	Name() string
	Check(ctx context.Context) error
}

var _ checker = nanoserver.Readiness{}

func TestReadinessFollowsTheApp(t *testing.T) {
	var failing error
	app := nano.NewApp("readiness-test", nano.AppConfig{})
	app.SetLogger(discard())
	app.Health("postgres", func(context.Context) error { return failing })
	var ready checker = nanoserver.NewReadiness(app)

	if ready.Name() != "readiness" {
		t.Fatalf("Name() = %q, want readiness", ready.Name())
	}
	if err := ready.Check(t.Context()); err == nil {
		t.Fatal("Check passed before the App started")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	if err := ready.Check(t.Context()); err != nil {
		t.Fatalf("Check on a running, healthy App = %v, want nil", err)
	}

	failing = errors.New("connection refused")
	if err := ready.Check(t.Context()); err == nil {
		t.Fatal("Check passed while a health check fails")
	}
}

// pinnedBody is a /health body that a gate flow asserts byte for byte.
const pinnedBody = `{"status":"HEALTHY","service":"billing","version":"1.2.3"}` + "\n"

func pinnedHealth(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if code != 0 {
			w.WriteHeader(code)
		}
		_, _ = io.WriteString(w, pinnedBody)
	})
}

// flushedHealth flushes before it writes, as a streaming handler does: the
// flush must not commit a 200.
func flushedHealth() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := http.NewResponseController(w).Flush(); err != nil {
			panic(err)
		}
		_, _ = io.WriteString(w, pinnedBody)
	})
}

func serveHealth(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	return rec
}

func TestReadinessMiddlewareSetsTheStatusAndKeepsTheBody(t *testing.T) {
	var failing error
	app := nano.NewApp("readiness-middleware-test", nano.AppConfig{})
	app.SetLogger(discard())
	app.Health("postgres", func(context.Context) error { return failing })
	gate := nanoserver.ReadinessMiddleware(app)

	check := func(when string, h http.Handler, wantCode int) {
		t.Helper()
		rec := serveHealth(h)
		if rec.Code != wantCode {
			t.Errorf("%s: status = %d, want %d", when, rec.Code, wantCode)
		}
		if rec.Body.String() != pinnedBody {
			t.Errorf("%s: body = %q, want the handler's body %q", when, rec.Body.String(), pinnedBody)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s: Content-Type = %q, want the handler's header", when, got)
		}
	}

	check("before start, implicit 200", gate(pinnedHealth(0)), http.StatusServiceUnavailable)
	check("before start, explicit 200", gate(pinnedHealth(http.StatusOK)), http.StatusServiceUnavailable)
	check("before start, flushed 200", gate(flushedHealth()), http.StatusServiceUnavailable)
	silent := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if rec := serveHealth(gate(silent)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before start, handler writes nothing: status = %d, want 503", rec.Code)
	}
	check("before start, handler 500", gate(pinnedHealth(http.StatusInternalServerError)),
		http.StatusInternalServerError)

	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	check("running", gate(pinnedHealth(0)), http.StatusOK)
	check("running, handler 500", gate(pinnedHealth(http.StatusInternalServerError)),
		http.StatusInternalServerError)

	failing = errors.New("connection refused")
	check("failing check", gate(pinnedHealth(http.StatusOK)), http.StatusServiceUnavailable)
}

// syncBuffer collects the server's error log, which net/http writes from
// the connection goroutine.
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

// Over a real connection, the middleware passes an informational status
// through, sends exactly one final status and never makes net/http log a
// superfluous WriteHeader, whatever order the handler writes in.
func TestReadinessMiddlewareWritesOneFinalStatus(t *testing.T) {
	app := nano.NewApp("readiness-writer-test", nano.AppConfig{})
	app.SetLogger(discard())
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pinnedBody[:10])
		_ = http.NewResponseController(w).Flush()
		_, _ = io.WriteString(w, pinnedBody[10:])
	})
	var serverLog syncBuffer
	srv := httptest.NewUnstartedServer(nanoserver.ReadinessMiddleware(app)(handler))
	srv.Config.ErrorLog = log.New(&serverLog, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)

	var informational []int
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, _ textproto.MIMEHeader) error {
		informational = append(informational, code)
		return nil
	}}
	ctx := httptrace.WithClientTrace(t.Context(), trace)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	srv.Close() // flushes the server's log

	if resp.StatusCode != http.StatusServiceUnavailable || string(body) != pinnedBody {
		t.Errorf("GET /health = %d %q, want 503 and the pinned body", resp.StatusCode, body)
	}
	if len(informational) != 1 || informational[0] != http.StatusEarlyHints {
		t.Errorf("informational statuses = %v, want [103]", informational)
	}
	if got := serverLog.String(); strings.Contains(got, "superfluous") {
		t.Errorf("server log = %q, want no superfluous WriteHeader", got)
	}
}

// The defect: a service with a pinned /health body kept answering 200 while
// it drained, so the load balancer kept routing to an instance that was
// about to close its listener.
func TestReadinessMiddlewareAnswers503WhileTheAppDrains(t *testing.T) {
	app := nano.NewApp("readiness-drain-test", nano.AppConfig{
		StopTimeout: 10 * time.Second,
		DrainDelay:  5 * time.Second,
	})
	app.SetLogger(discard())
	mux := http.NewServeMux()
	mux.Handle("GET /health", nanoserver.ReadinessMiddleware(app)(pinnedHealth(0)))
	mux.HandleFunc("GET /widgets", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := nanoserver.NewServer(testConfig(), mux, discard())
	app.Manage("httpserver", srv)

	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-srv.Listening()
	base := "http://" + srv.Addr().String()
	if code, body := get(t, base+"/health"); code != http.StatusOK || body != pinnedBody {
		t.Fatalf("running: GET /health = %d %q, want 200 and the pinned body", code, body)
	}

	stopped := make(chan error, 1)
	stopCtx, cancelStop := context.WithCancel(context.Background())
	go func() { stopped <- app.Stop(stopCtx) }()
	t.Cleanup(func() {
		cancelStop() // ends the drain delay early
		if err := <-stopped; err != nil {
			t.Errorf("Stop = %v, want nil", err)
		}
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		code, body := get(t, base+"/health")
		if code == http.StatusServiceUnavailable {
			if body != pinnedBody {
				t.Fatalf("draining: body = %q, want the pinned body", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("draining: GET /health = %d, want 503", code)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code, _ := get(t, base+"/widgets"); code != http.StatusOK {
		t.Fatalf("draining: GET /widgets = %d, want 200: the server still serves", code)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}
