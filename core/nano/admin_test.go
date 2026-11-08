package nano

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func probe(t *testing.T, app *App, path string) (int, Report) {
	t.Helper()
	rr := httptest.NewRecorder()
	app.AdminHandler().ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	var rep Report
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatalf("%s body %q: %v", path, rr.Body.String(), err)
	}
	return rr.Code, rep
}

func TestReadyzFollowsLifecycleAndChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var dbErr error
		inStop := make(chan struct{})
		release := make(chan struct{})
		app := newTestApp(AppConfig{})
		app.Health("db", func(context.Context) error { return dbErr })
		app.OnStop("slow", func(context.Context) error { close(inStop); <-release; return nil })

		if code, rep := probe(t, app, "/readyz"); code != http.StatusServiceUnavailable || rep.State != "new" {
			t.Fatalf("before start: %d %+v", code, rep)
		}
		if code, _ := probe(t, app, "/livez"); code != http.StatusOK {
			t.Fatalf("livez before start = %d", code)
		}

		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		code, rep := probe(t, app, "/readyz")
		if code != http.StatusOK || rep.Status != "ok" || rep.Checks["db"] != "ok" {
			t.Fatalf("running: %d %+v", code, rep)
		}

		dbErr = errors.New("connection refused")
		code, rep = probe(t, app, "/readyz")
		if code != http.StatusServiceUnavailable || rep.Checks["db"] != "fail" {
			t.Fatalf("failing check: %d %+v", code, rep)
		}
		dbErr = nil

		stopped := make(chan error, 1)
		go func() { stopped <- app.Stop(t.Context()) }()
		<-inStop
		code, rep = probe(t, app, "/readyz")
		if code != http.StatusServiceUnavailable || rep.State != "stopping" {
			t.Fatalf("while stopping: %d %+v", code, rep)
		}
		close(release)
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if code, _ = probe(t, app, "/livez"); code != http.StatusServiceUnavailable {
			t.Fatalf("livez after stop = %d", code)
		}
	})
}

func TestReadyzCheckTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		app := newTestApp(AppConfig{HealthTimeout: time.Second})
		app.Health("hung", func(context.Context) error { <-release; return nil })
		app.Health("fine", func(context.Context) error { return nil })
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		err := app.Ready(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Ready = %v, want deadline exceeded", err)
		}
		if took := time.Since(began); took != time.Second {
			t.Fatalf("Ready took %v, want the 1s health timeout", took)
		}
		close(release)
		_ = app.Stop(t.Context())
	})
}

// /readyz is unauthenticated, so it says only which check failed. The error,
// which may hold an internal address or a database user, goes to the log,
// once per change, and Ready keeps it for callers in the process.
func TestReadyzKeepsCheckErrorsInTheLog(t *testing.T) {
	var logs bytes.Buffer
	dbErr := errors.New("dial tcp 10.0.1.23:5432: connect: connection refused")
	app := NewApp("test", AppConfig{})
	app.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	app.Health("db", func(context.Context) error { return dbErr })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	rr := httptest.NewRecorder()
	for range 3 {
		rr = httptest.NewRecorder()
		app.AdminHandler().ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
	}
	if strings.Contains(rr.Body.String(), "10.0.1.23") || !strings.Contains(rr.Body.String(), `"db":"fail"`) {
		t.Fatalf("/readyz body = %s, want the check named without its error", rr.Body)
	}
	if n := strings.Count(logs.String(), "10.0.1.23"); n != 1 {
		t.Fatalf("the error is logged %d times over three probes, want once:\n%s", n, logs.String())
	}
	if err := app.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "10.0.1.23") {
		t.Fatalf("Ready = %v, want the detailed error", err)
	}

	dbErr = errors.New("dial tcp 10.0.1.24:5432: connect: connection refused")
	_ = app.Ready(t.Context())
	if !strings.Contains(logs.String(), "10.0.1.24") {
		t.Fatal("a different error is logged again")
	}
}
