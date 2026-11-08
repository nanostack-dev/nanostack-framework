package nano

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	nanolog "github.com/nanostack-dev/nanostack-framework/core/log"
)

// Report is the body of /livez and /readyz.
type Report struct {
	App    string            `json:"app"`
	State  string            `json:"state"`
	Status string            `json:"status"` // "ok" or "unavailable"
	Checks map[string]string `json:"checks,omitempty"`
}

// AdminHandler serves the App's probes:
//
//   - GET /livez is liveness: 200 until the App has stopped. The name is
//     the Kubernetes one, which replaces the deprecated /healthz.
//   - GET /readyz is readiness: 200 only while the App is running and every
//     health check passes, 503 otherwise, including during shutdown so load
//     balancers drain the instance first.
//
// The App does not listen itself; mount the handler on an admin listener, not
// on the API's. The body names each check with "ok" or "fail" only: a check's
// error can hold an internal address or a database user. The App logs that
// error when the check starts failing or fails differently, and Ready returns
// it to callers in the process.
func (a *App) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", a.serveLivez)
	mux.HandleFunc("GET /readyz", a.serveReadyz)
	return mux
}

func (a *App) currentState() state {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *App) serveLivez(w http.ResponseWriter, _ *http.Request) {
	st := a.currentState()
	rep := Report{App: a.name, State: st.String(), Status: "ok"}
	code := http.StatusOK
	if st == stateStopped {
		rep.Status, code = "unavailable", http.StatusServiceUnavailable
	}
	writeReport(w, code, rep)
}

func (a *App) serveReadyz(w http.ResponseWriter, r *http.Request) {
	results, err := a.evaluate(r.Context())
	rep := Report{App: a.name, State: a.currentState().String(), Status: "ok", Checks: results}
	code := http.StatusOK
	if err != nil {
		rep.Status, code = "unavailable", http.StatusServiceUnavailable
	}
	writeReport(w, code, rep)
}

// Ready reports whether the App is running and every health check passes.
// The error joins each failing check; it is nil when the App is ready.
func (a *App) Ready(ctx context.Context) error {
	_, err := a.evaluate(ctx)
	return err
}

func (a *App) evaluate(ctx context.Context) (map[string]string, error) {
	a.mu.Lock()
	st := a.state
	checks := append([]check(nil), a.checks...)
	a.mu.Unlock()

	var notRunning error
	if st != stateRunning {
		notRunning = fmt.Errorf("nano: app %s is %s", a.name, st)
	}

	ctx, cancel := context.WithTimeout(ctx, a.cfg.HealthTimeout)
	defer cancel()

	results := make(map[string]string, len(checks))
	errs := make([]error, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Go(func() {
			if err := call(ctx, c.check); err != nil {
				errs[i] = fmt.Errorf("nano: check %s: %w", c.name, err)
			}
		})
	}
	wg.Wait()
	for i, c := range checks {
		key := c.name
		for n := 2; ; n++ { // Two checks may share a name; keep both results.
			if _, taken := results[key]; !taken {
				break
			}
			key = fmt.Sprintf("%s#%d", c.name, n)
		}
		results[key] = "ok"
		if errs[i] != nil {
			results[key] = "fail"
		}
		a.noteCheck(ctx, key, errs[i])
	}
	return results, errors.Join(append([]error{notRunning}, errs...)...)
}

// noteCheck logs the error of a check when it changes: a passing check
// starts failing, or a failing one fails with another error. A probe every
// few seconds then logs a failure once, not on every probe.
func (a *App) noteCheck(ctx context.Context, key string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.checkMu.Lock()
	if a.lastChecks == nil {
		a.lastChecks = map[string]string{}
	}
	last, seen := a.lastChecks[key]
	a.lastChecks[key] = msg
	a.checkMu.Unlock()
	switch {
	case msg == last && (seen || msg == ""):
	case msg == "":
		a.logger().InfoContext(ctx, "nano: health check passes again", slog.String("check", key))
	default:
		a.logger().WarnContext(ctx, "nano: health check failed", slog.String("check", key), nanolog.Err(err))
	}
}

func writeReport(w http.ResponseWriter, code int, rep Report) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.MarshalWrite(w, rep)
}
