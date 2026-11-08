package log_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
)

func serve(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHTTPMiddlewareWritesSummaryWithRoutePattern(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "loading flow")
		_, _ = io.WriteString(w, "hello")
	})
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(mux)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/flows/123?expand=steps", nil)
	req.Header.Set("User-Agent", "test")
	req.RemoteAddr = "10.0.0.2:51234"                               // the ingress, a trusted proxy by default
	req.Header.Set("X-Forwarded-For", "198.51.100.66, 203.0.113.7") // the client sent the first hop
	resp := serve(t, h, req)

	requestID := resp.Header().Get(log.RequestIDHeader)
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("minted request id %q: %v", requestID, err)
	}
	inner := rec.AssertLogged(t, slog.LevelInfo, "loading flow", slog.String(log.KeyRequestID, requestID))
	if inner.String("path") != "/flows/123" || inner.String("method") != http.MethodGet {
		t.Fatalf("handler line lacks request fields: %s", inner.Format())
	}
	summary := rec.AssertLogged(t, slog.LevelInfo, "http request",
		slog.String(log.KeyRequestID, requestID),
		slog.String("method", http.MethodGet),
		slog.String("path", "/flows/123"),
		slog.String("route", "GET /flows/{id}"),
		slog.Int("status", http.StatusOK),
		slog.Int("bytes_out", 5),
		slog.String("client_ip", "203.0.113.7"),
		slog.String("query.expand", "steps"),
		slog.String("user_agent", "test"),
	)
	if d, ok := summary.Value("duration"); !ok || d.Kind() != slog.KindDuration {
		t.Fatalf("duration = %v", d)
	}
	if summary.PC != 0 {
		t.Fatal("summary line carries a meaningless source")
	}
}

type principalKey struct{}

func TestHTTPMiddlewareCarriesAttrsSetByInnerMiddleware(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A real auth middleware also replaces the request; the summary
			// line, written from the outer request, must still see the org.
			ctx := context.WithValue(r.Context(), principalKey{}, "u1")
			log.SetAttrs(ctx, slog.String(log.KeyOrgID, "org_1"), slog.String(log.KeyUserID, "u1"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(auth(app))

	serve(t, h, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/flows/1", nil))

	rec.AssertLogged(t, slog.LevelInfo, "http request",
		slog.String(log.KeyOrgID, "org_1"), slog.String(log.KeyUserID, "u1"), slog.Int("status", http.StatusNoContent))
}

func TestHTTPMiddlewareRequestIDHeader(t *testing.T) {
	t.Parallel()
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: slog.New(slog.DiscardHandler)})(http.NotFoundHandler())
	tests := []struct {
		name, inbound string
		reused        bool
	}{
		{"well formed", "edge-req-42", true},
		{"too long", strings.Repeat("a", 129), false},
		{"control characters", "abc\ninjected", false},
		{"spaces", "a b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.Header.Set(log.RequestIDHeader, tt.inbound)
			got := serve(t, h, req).Header().Get(log.RequestIDHeader)
			if (got == tt.inbound) != tt.reused || got == "" {
				t.Fatalf("response id %q for inbound %q", got, tt.inbound)
			}
		})
	}
}

func TestHTTPMiddlewareLevels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		cancel bool
		want   slog.Level
	}{
		{"ok", http.StatusOK, false, slog.LevelInfo},
		{"client error", http.StatusNotFound, false, slog.LevelInfo},
		{"server error", http.StatusBadGateway, false, slog.LevelError},
		{"client cancel beats 5xx", http.StatusServiceUnavailable, true, slog.LevelWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, rec := logtest.New()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.cancel {
					cancel() // the client disconnects while the handler runs
				}
				w.WriteHeader(tt.status)
			})
			h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app)

			serve(t, h, httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil))

			entry := rec.AssertLogged(t, tt.want, "http request", slog.Int("status", tt.status))
			if _, canceled := entry.Value("client_canceled"); canceled != tt.cancel {
				t.Fatalf("client_canceled present = %v: %s", canceled, entry.Format())
			}
		})
	}
}

func TestHTTPMiddlewareSkipSummaryKeepsScope(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	app := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "probe handled")
	})
	h := log.HTTPMiddleware(log.HTTPOptions{
		Logger:      logger,
		SkipSummary: func(r *http.Request) bool { return r.URL.Path == "/health" },
	})(app)

	serve(t, h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))

	rec.AssertNotLogged(t, "http request")
	if rec.AssertLogged(t, slog.LevelInfo, "probe handled").String(log.KeyRequestID) == "" {
		t.Fatal("skipped request lost its id")
	}
}

func TestHTTPMiddlewareKeepsStreamingWorking(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			t.Errorf("SetWriteDeadline through the recorder: %v", err)
		}
		_, _ = io.WriteString(w, "data: 1\n\n")
		if err := rc.Flush(); err != nil {
			t.Errorf("Flush through the recorder: %v", err)
		}
	})
	srv := httptest.NewTestServer(t, log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://flows.test/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// The response ends after the middleware returns, so once the body is read
	// the summary line has been written.
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "data: 1\n\n" {
		t.Fatalf("body = %q", body)
	}
	rec.AssertLogged(t, slog.LevelInfo, "http request", slog.Int("bytes_out", len(body)))
}
