package nanohttp_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
)

// The router benchmarks compare nanohttp.NewRouter on a ServeMux with chi v5,
// the router of modules/httpserver, on the same 30 routes. The handlers read
// one path parameter, as a generated handler does, and write a status.
//
// impl=legacy is chi and impl=beta is NewRouter, both with the same trivial
// NotFound and MethodNotAllowed handlers, so the rows measure routing alone.
// impl=beta-problem keeps NewRouter's default handlers, which write an RFC
// 9457 problem and log it through slog.Default (an io.Discard JSON handler at
// Info here, so the Debug line of a 4xx is not written). impl=servemux is the
// bare ServeMux with its own text/plain 404 and 405: the floor NewRouter
// builds on.

// benchRoute is one operation of the benchmark API.
type benchRoute struct {
	method string
	path   string // with {param} segments, the syntax of both routers
}

// benchRoutes returns 30 operations shaped like a service's REST API.
func benchRoutes() []benchRoute {
	var routes []benchRoute
	for _, resource := range []string{"flows", "monitors", "environments", "secrets", "webhooks"} {
		routes = append(routes,
			benchRoute{http.MethodGet, "/v1/" + resource},
			benchRoute{http.MethodPost, "/v1/" + resource},
			benchRoute{http.MethodGet, "/v1/" + resource + "/{id}"},
			benchRoute{http.MethodPatch, "/v1/" + resource + "/{id}"},
			benchRoute{http.MethodDelete, "/v1/" + resource + "/{id}"},
			benchRoute{http.MethodGet, "/v1/" + resource + "/{id}/runs/{runID}"},
		)
	}
	return routes
}

type benchResponseWriter struct {
	header http.Header
	status int
}

func (w *benchResponseWriter) Header() http.Header         { return w.header }
func (w *benchResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *benchResponseWriter) WriteHeader(status int)      { w.status = status }

func statusOnly(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
}

func newChiRouter() http.Handler {
	r := chi.NewRouter()
	r.NotFound(statusOnly(http.StatusNotFound).ServeHTTP)
	r.MethodNotAllowed(statusOnly(http.StatusMethodNotAllowed).ServeHTTP)
	for _, route := range benchRoutes() {
		r.Method(route.method, route.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if chi.URLParam(r, "id") == "" && strings.Contains(route.path, "{id}") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
	}
	return r
}

func newServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, route := range benchRoutes() {
		mux.HandleFunc(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("id") == "" && strings.Contains(route.path, "{id}") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	}
	return mux
}

func BenchmarkRouter(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })

	routers := []struct {
		name    string
		handler http.Handler
	}{
		{"impl=legacy", newChiRouter()},
		{"impl=beta", nanohttp.NewRouter(newServeMux(), nanohttp.RouterOptions{
			NotFound:         statusOnly(http.StatusNotFound),
			MethodNotAllowed: statusOnly(http.StatusMethodNotAllowed),
		})},
		{"impl=beta-problem", nanohttp.NewRouter(newServeMux(), nanohttp.RouterOptions{})},
		{"impl=servemux", newServeMux()},
	}
	cases := []struct {
		name   string
		method string
		target string
		want   int
	}{
		{"case=hit", http.MethodGet, "/v1/webhooks/wh_123/runs/run_456", http.StatusOK},
		{"case=404", http.MethodGet, "/v1/webhooks/wh_123/unknown", http.StatusNotFound},
		{"case=405", http.MethodPut, "/v1/webhooks/wh_123", http.StatusMethodNotAllowed},
	}
	for _, router := range routers {
		for _, c := range cases {
			b.Run(router.name+"/"+c.name, func(b *testing.B) {
				r := httptest.NewRequest(c.method, c.target, http.NoBody)
				w := &benchResponseWriter{header: make(http.Header)}
				b.ReportAllocs()
				for b.Loop() {
					router.handler.ServeHTTP(w, r)
				}
				if w.status != c.want {
					b.Fatalf("status = %d, want %d", w.status, c.want)
				}
			})
		}
	}
}
