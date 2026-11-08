package nanohttp_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
)

// A 404 or a 405 has no route: the summary line names a route only for a
// request that a pattern served.
func TestRouterMissHasNoRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, target string
		status         int
	}{
		{http.MethodGet, "/nothing/here", http.StatusNotFound},
		{http.MethodPut, "/flows/f1", http.StatusMethodNotAllowed},
		{http.MethodGet, "/flows/f1", http.StatusOK},
	} {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(nanohttp.NewRouter(routes(), nanohttp.RouterOptions{}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))
		if rec.Code != tc.status {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.target, rec.Code, tc.status)
		}
		hasRoute := strings.Contains(buf.String(), `"route":`)
		if hasRoute != (tc.status == http.StatusOK) {
			t.Errorf("%s %s: route logged = %v: %s", tc.method, tc.target, hasRoute, buf.String())
		}
	}
}

// A miss costs about what a bare ServeMux miss costs: the router looks the
// path up once per probed method only to build Allow, never again per
// method inside ServeMux.
func TestRouterMissAllocatesLikeServeMux(t *testing.T) {
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{})
	mux := routes()
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/flows/f1/unknown"},
		{http.MethodPut, "/flows/f1"},
	} {
		req := httptest.NewRequest(tc.method, tc.target, nil)
		bare := testing.AllocsPerRun(50, func() { mux.ServeHTTP(httptest.NewRecorder(), req) })
		got := testing.AllocsPerRun(50, func() { rt.ServeHTTP(httptest.NewRecorder(), req) })
		if got > 2*bare+20 {
			t.Errorf("%s %s: router %.0f allocs, bare ServeMux %.0f", tc.method, tc.target, got, bare)
		}
		t.Logf("%s %s: router %.0f allocs, bare ServeMux %.0f", tc.method, tc.target, got, bare)
	}
}

// A mux with a pattern that matches every path never misses, so NewRouter
// hands it back unchanged.
func TestRouterLeavesAMuxThatNeverMisses(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"/", "/{path...}"} {
		mux := http.NewServeMux()
		mux.Handle(pattern, statusHandler(http.StatusTeapot, "mine"))
		rt := nanohttp.NewRouter(mux, nanohttp.RouterOptions{})
		if rec := do(rt, http.MethodPost, "/anything"); rec.Code != http.StatusTeapot {
			t.Errorf("%s: POST /anything = %d, want the mux's own handler", pattern, rec.Code)
		}
	}
}

func TestRouterRefusesToWrapAMuxTwice(t *testing.T) {
	t.Parallel()
	mux := routes()
	_ = nanohttp.NewRouter(mux, nanohttp.RouterOptions{})
	defer func() {
		if recover() == nil {
			t.Fatal("a second NewRouter on the same mux did not panic")
		}
	}()
	_ = nanohttp.NewRouter(mux, nanohttp.RouterOptions{})
}
