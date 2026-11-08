package log_test

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestHTTPMiddlewareRedactsQueryWhateverTheLogger(t *testing.T) {
	t.Parallel()
	// A plain JSON handler, without the framework's redaction stage: the
	// middleware must not rely on it.
	var out syncBuffer
	plain := slog.New(slog.NewJSONHandler(&out, nil))
	logger, rec := logtest.New()
	target := "/reset?token=QLEAK&api_key=QLEAK2&password=QLEAK3&X-Amz-Signature=QLEAK4&page=2&tag=a&tag=b"

	for _, l := range []*slog.Logger{plain, logger} {
		h := log.HTTPMiddleware(log.HTTPOptions{Logger: l})(http.NotFoundHandler())
		serve(t, h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
	}

	line := onlyLine(t, out.String())
	query, _ := line["query"].(map[string]any)
	if query["token"] != secret.Redacted || query["api_key"] != secret.Redacted || query["page"] != "2" {
		t.Fatalf("query = %v", line["query"])
	}
	entry := rec.AssertLogged(t, slog.LevelInfo, "http request",
		slog.String("query.token", secret.Redacted),
		slog.String("query.api_key", secret.Redacted),
		slog.String("query.password", secret.Redacted),
		slog.String("query.X-Amz-Signature", secret.Redacted),
		slog.String("query.page", "2"),
		slog.Any("query.tag", []string{"a", "b"}),
	)
	for _, got := range []string{out.String(), entry.Format()} {
		if strings.Contains(got, "QLEAK") {
			t.Fatalf("query leaked: %s", got)
		}
	}
}

func TestHTTPMiddlewareClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, remote, forwarded, realIP string
		trusted                         []netip.Prefix
		want                            string
	}{
		{name: "direct client", remote: "198.51.100.1:5000", want: "198.51.100.1"},
		{
			name:      "direct client spoofs the header",
			remote:    "198.51.100.1:5000",
			forwarded: "1.2.3.4",
			want:      "198.51.100.1",
		},
		{name: "behind the ingress", remote: "10.0.0.2:80", forwarded: "203.0.113.7", want: "203.0.113.7"},
		{
			name: "spoofed hop left of the real one", remote: "10.0.0.2:80",
			forwarded: "1.2.3.4, 203.0.113.7", want: "203.0.113.7",
		},
		{
			name: "chain of trusted proxies", remote: "10.0.0.2:80",
			forwarded: "1.2.3.4, 203.0.113.7, 10.0.0.9", want: "203.0.113.7",
		},
		{
			name:      "garbage hop stops the walk",
			remote:    "10.0.0.2:80",
			forwarded: "203.0.113.7, nonsense",
			want:      "10.0.0.2",
		},
		{name: "real ip from the ingress", remote: "10.0.0.2:80", realIP: "203.0.113.8", want: "203.0.113.8"},
		{name: "real ip from a client", remote: "198.51.100.1:5000", realIP: "203.0.113.8", want: "198.51.100.1"},
		{
			name: "no proxy trusted", remote: "10.0.0.2:80", forwarded: "203.0.113.7",
			trusted: []netip.Prefix{}, want: "10.0.0.2",
		},
		{
			name: "custom proxy network", remote: "100.64.0.5:80", forwarded: "203.0.113.7",
			trusted: []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")}, want: "203.0.113.7",
		},
		{name: "ipv6 ingress", remote: "[fd00::1]:80", forwarded: "2001:db8::7", want: "2001:db8::7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, rec := logtest.New()
			h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger, TrustedProxies: tt.trusted})(http.NotFoundHandler())
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-Ip", tt.realIP)
			}
			serve(t, h, req)
			rec.AssertLogged(t, slog.LevelInfo, "http request", slog.String("client_ip", tt.want))
		})
	}
}

// hijackableWriter is a ResponseWriter whose connection can be hijacked, as
// net/http's HTTP/1 writer can.
type hijackableWriter struct {
	*httptest.ResponseRecorder

	conn net.Conn
}

func (w hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestHTTPMiddlewareKeepsHijackWorking(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// gorilla/websocket v1.5.3 does exactly this type assertion.
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("the recorder hides http.Hijacker")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		go func() {
			_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n\r\n")
			_ = conn.Close()
		}()
	})
	h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app)

	w := hijackableWriter{ResponseRecorder: httptest.NewRecorder(), conn: server}
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws", nil))
	got, _ := io.ReadAll(client)

	if !strings.HasPrefix(string(got), "HTTP/1.1 101") {
		t.Fatalf("client read %q", got)
	}
	rec.AssertLogged(t, slog.LevelInfo, "http request",
		slog.Int("status", http.StatusSwitchingProtocols), slog.Bool("hijacked", true), slog.Int("bytes_out", 0))
}

func TestHTTPMiddlewareHijackUnsupported(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("the recorder hides http.Hijacker")
		}
		if _, _, err := hijacker.Hijack(); !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("Hijack on a writer that cannot: %v", err)
		}
		w.WriteHeader(http.StatusBadRequest)
	})
	serve(t, log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws", nil))

	entry := rec.AssertLogged(t, slog.LevelInfo, "http request", slog.Int("status", http.StatusBadRequest))
	if _, hijacked := entry.Value("hijacked"); hijacked {
		t.Fatalf("failed hijack logged as hijacked: %s", entry.Format())
	}
}

// readerFromWriter records whether ReadFrom was used.
type readerFromWriter struct {
	*httptest.ResponseRecorder

	used *bool
}

func (w readerFromWriter) ReadFrom(src io.Reader) (int64, error) {
	*w.used = true
	return io.Copy(w.ResponseRecorder, src)
}

func TestHTTPMiddlewareForwardsReadFrom(t *testing.T) {
	t.Parallel()
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(io.ReaderFrom); !ok {
			t.Error("the recorder hides io.ReaderFrom")
		}
		// A reader without WriteTo, as http.ServeContent passes through io.CopyN.
		_, _ = io.Copy(w, io.LimitReader(strings.NewReader("file body"), 100))
	})
	for _, forward := range []bool{true, false} {
		logger, rec := logtest.New()
		h := log.HTTPMiddleware(log.HTTPOptions{Logger: logger})(app)
		used := false
		var w http.ResponseWriter = httptest.NewRecorder()
		if forward {
			w = readerFromWriter{ResponseRecorder: httptest.NewRecorder(), used: &used}
		}
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/file", nil))
		if used != forward {
			t.Errorf("forward=%v: wrapped ReadFrom used = %v", forward, used)
		}
		rec.AssertLogged(t, slog.LevelInfo, "http request", slog.Int("bytes_out", len("file body")))
	}
}

func panickingHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("write") != "" {
		w.WriteHeader(http.StatusAccepted)
	}
	if r.URL.Query().Get("abort") != "" {
		panic(http.ErrAbortHandler)
	}
	panic("boom")
}

func TestHTTPMiddlewareLogsPanics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, target string
		level        slog.Level
		status       int
		value        any
	}{
		{"before writing", "/p", slog.LevelError, http.StatusInternalServerError, "boom"},
		{"after writing", "/p?write=1", slog.LevelError, http.StatusAccepted, "boom"},
		{"skipped path still logged", "/health", slog.LevelError, http.StatusInternalServerError, "boom"},
		{"abort handler", "/p?abort=1", slog.LevelWarn, http.StatusInternalServerError, http.ErrAbortHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, rec := logtest.New()
			h := log.HTTPMiddleware(log.HTTPOptions{
				Logger:      logger,
				SkipSummary: func(r *http.Request) bool { return r.URL.Path == "/health" },
			})(http.HandlerFunc(panickingHandler))

			var recovered any
			var stack string
			func() {
				defer func() {
					recovered = recover()
					stack = string(debug.Stack())
				}()
				serve(t, h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil))
			}()

			if recovered != tt.value {
				t.Fatalf("the panic did not propagate: recovered %v", recovered)
			}
			if !strings.Contains(stack, "panickingHandler") {
				t.Errorf("the re-panic lost the original stack:\n%s", stack)
			}
			rec.AssertLogged(t, tt.level, "http request",
				slog.Int("status", tt.status), slog.Any("panic", tt.value))
		})
	}
}
