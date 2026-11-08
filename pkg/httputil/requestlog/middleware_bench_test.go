package requestlog_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog" //nolint:depguard // the v0.22 baseline: the zerolog path services run today

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/requestlog"
)

// The request logging benchmarks compare the beta middleware with the v0.22
// chain that services run today: requestlog.Contextualize (request id and the
// request-scoped zerolog logger) followed by requestlog.New (the summary
// line). log.HTTPMiddleware does both jobs, so it is measured against both
// legacy middlewares together.
//
// Both sides write JSON to io.Discard at Info, with the logger a production
// service builds: log.New for core/log, and modules/logging's
// zerolog.New(...).With().Timestamp().Caller() for the legacy side. An inner
// middleware adds org_id and user_id the way an auth middleware does, so the
// summary line carries them on both sides. The request carries an inbound
// X-Request-Id, so neither side mints a UUID, and comes through a private
// proxy address with X-Forwarded-For, so core/log walks its trusted-proxy
// list.
//
// The file sits in the legacy tree because only a legacy package may import
// both sides: internal/layers forbids core from importing pkg or zerolog,
// test files included.
//
// Sub-benchmark names carry impl=<side>, so `benchstat -col /impl` puts the
// sides next to each other.

// benchResponseWriter is a reusable ResponseWriter that discards the body.
type benchResponseWriter struct {
	header http.Header
	status int
}

func (w *benchResponseWriter) Header() http.Header         { return w.header }
func (w *benchResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *benchResponseWriter) WriteHeader(status int)      { w.status = status }

func newBenchRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/flows/flw_123/runs?limit=20&cursor=abc", http.NoBody)
	r.RemoteAddr = "10.0.0.7:41234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.2")
	r.Header.Set("User-Agent", "echopoint-cli/1.4.0")
	r.Header.Set(log.RequestIDHeader, "01HZX3V6Q8K2J7M4N5P6R7S8T9")
	return r
}

func statusHandler(status int) http.Handler {
	body := []byte(`{"id":"flw_123","status":"ok"}`)
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

// betaAuth adds the organization and the user to the request scope.
func betaAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.SetAttrs(r.Context(), slog.String(log.KeyOrgID, "org_42"), slog.String(log.KeyUserID, "usr_7"))
		next.ServeHTTP(w, r)
	})
}

// legacyAuth adds the organization and the user to the request logger.
func legacyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestlog.From(r.Context()).UpdateContext(func(c zerolog.Context) zerolog.Context {
			return c.Str("org_id", "org_42").Str("user_id", "usr_7")
		})
		next.ServeHTTP(w, r)
	})
}

func betaLogger(wire log.Wire) *slog.Logger {
	return log.New(log.Config{
		Output:      io.Discard,
		Wire:        wire,
		Service:     "bench",
		Environment: "production",
		Version:     "1.0.0",
	})
}

func legacyLogger() zerolog.Logger {
	return zerolog.New(io.Discard).Level(zerolog.InfoLevel).With().Timestamp().Caller().Logger()
}

func BenchmarkRequestLog(b *testing.B) {
	for _, status := range []struct {
		name string
		code int
	}{{"status=200", http.StatusOK}, {"status=500", http.StatusInternalServerError}} {
		chains := []struct {
			name    string
			handler http.Handler
		}{
			{
				name: "impl=legacy",
				handler: requestlog.Contextualize(legacyLogger())(
					legacyAuth(requestlog.New(legacyLogger(), requestlog.Options{})(statusHandler(status.code))),
				),
			},
			{
				name: "impl=beta",
				handler: log.HTTPMiddleware(log.HTTPOptions{Logger: betaLogger(log.WireSlog)})(
					betaAuth(statusHandler(status.code)),
				),
			},
			{
				name: "impl=beta-legacywire",
				handler: log.HTTPMiddleware(log.HTTPOptions{Logger: betaLogger(log.WireLegacy), LegacySummary: true})(
					betaAuth(statusHandler(status.code)),
				),
			},
		}
		for _, chain := range chains {
			b.Run(chain.name+"/"+status.name, func(b *testing.B) {
				r := newBenchRequest()
				w := &benchResponseWriter{header: make(http.Header)}
				b.ReportAllocs()
				for b.Loop() {
					chain.handler.ServeHTTP(w, r)
				}
				if w.status != status.code {
					b.Fatalf("status = %d, want %d", w.status, status.code)
				}
			})
		}
	}
}
