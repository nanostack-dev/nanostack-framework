package nanoserver

import (
	"context"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// ReadinessName is the key of the readiness result in a legacy /health body.
const ReadinessName = "readiness"

// Readiness reports the App's readiness to the legacy /health endpoint.
//
// /health stays a contract in the beta: it is the Docker HEALTHCHECK and gate
// flows assert it. Its body comes from pkg/health, which a layered package may
// not import. Readiness has the method set of pkg/health.Checker (Name and
// Check), so the service registers it in its own registry and keeps one
// implementation of the body:
//
//	registry := health.NewRegistry()
//	registry.Register(nanoserver.NewReadiness(app))
//	mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Registry: registry}))
//
// The body then has "readiness": "HEALTHY" while the App runs and every
// nano HealthChecker passes, and the endpoint answers 503 otherwise. That
// key replaces the per-dependency keys, such as "postgres", that the fx
// modules registered.
//
// A service whose /health body is pinned uses ReadinessMiddleware instead:
// it sets the status and leaves the body alone.
type Readiness struct {
	app *nano.App
}

// NewReadiness returns the readiness check of app.
func NewReadiness(app *nano.App) Readiness { return Readiness{app: app} }

// Name returns ReadinessName.
func (Readiness) Name() string { return ReadinessName }

// Check returns nil when the App runs and every health check passes, and the
// joined failures otherwise.
func (r Readiness) Check(ctx context.Context) error { return r.app.Ready(ctx) }

// ReadinessMiddleware returns a middleware that gives the wrapped handler the
// App's readiness as its status code, and nothing else.
//
// It is the choice for a service whose /health body is pinned, such as by a
// gate flow, and so cannot take the "readiness" key of Readiness. The
// service wraps its /health handler, and the body stays byte for byte the
// same:
//
//	router.With(nanoserver.ReadinessMiddleware(app)).Get("/health", healthHandler)
//
// While the App runs and every nano HealthChecker passes, the response is
// the handler's own. Otherwise, such as while the App starts or drains, a
// success or redirect status of the handler, or no status at all, becomes
// 503 Service Unavailable, so a load balancer and the Docker HEALTHCHECK take
// the instance out. An error status of the handler (400 and above) stays: the
// handler has already reported a failure.
func ReadinessMiddleware(app *nano.App) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if app.Ready(r.Context()) == nil {
				next.ServeHTTP(w, r)
				return
			}
			uw := &unavailableWriter{ResponseWriter: w}
			next.ServeHTTP(uw, r)
			if !uw.wroteHeader {
				// The handler wrote nothing: net/http would answer 200.
				uw.WriteHeader(http.StatusOK)
			}
		})
	}
}

// unavailableWriter turns a success or redirect status into 503 and passes
// the body through unchanged.
type unavailableWriter struct {
	http.ResponseWriter

	wroteHeader bool
}

func (w *unavailableWriter) WriteHeader(code int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	// 1xx responses are informational and precede the final status.
	if code >= http.StatusContinue && code < http.StatusOK {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wroteHeader = true
	if code < http.StatusBadRequest {
		code = http.StatusServiceUnavailable
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *unavailableWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// Flush sends the status first, so that a flush cannot commit the handler's
// 200 behind the writer's back.
func (w *unavailableWriter) Flush() { _ = w.FlushError() }

// FlushError is Flush for http.ResponseController.
func (w *unavailableWriter) FlushError() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *unavailableWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
