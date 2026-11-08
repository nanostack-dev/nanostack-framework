package nano

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		if code != http.StatusServiceUnavailable || rep.Checks["db"] != "nano: check db: connection refused" {
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
