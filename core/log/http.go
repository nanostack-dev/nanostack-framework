package log

import (
	"bufio"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// RequestIDHeader carries the request id, inbound and on the response.
const RequestIDHeader = "X-Request-Id"

// maxRequestIDLength bounds an inbound request id. A longer or non-printable
// value is replaced, so a client cannot inject lines or bloat every log entry.
const maxRequestIDLength = 128

// HTTPOptions configures HTTPMiddleware.
type HTTPOptions struct {
	// Logger writes the summary lines. Nil means slog.Default() at the time
	// of each request, so a logger installed after the router is built is
	// used.
	Logger *slog.Logger
	// SkipSummary, when it returns true, suppresses the summary line of a
	// request; the request still gets its id and scope. Use it for health
	// probes and long polls, whose lines add volume without signal. A request
	// whose handler panics is logged anyway.
	SkipSummary func(*http.Request) bool
	// TrustedProxies are the networks of the reverse proxies whose
	// X-Forwarded-For and X-Real-Ip headers client_ip may use. Nil means the
	// loopback and private networks (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12,
	// 192.168.0.0/16, ::1/128, fc00::/7), where a cluster's ingress runs. A
	// non-nil empty slice trusts no proxy, so client_ip is always the
	// connection address.
	TrustedProxies []netip.Prefix
}

// defaultTrustedProxies are the networks HTTPOptions.TrustedProxies defaults to.
var defaultTrustedProxies = []netip.Prefix{ //nolint:gochecknoglobals // read-only table.
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

// HTTPMiddleware returns middleware that opens the log scope of each request
// and writes one summary line when the handler returns or panics.
//
// Before the handler runs, it:
//   - reuses a well-formed inbound X-Request-Id or mints a UUIDv7, and echoes
//     it on the response;
//   - opens a scope (see WithScope) carrying request_id, method and path, so
//     every line logged with the request context has them. Middleware further
//     in, such as auth, adds the organization and user with SetAttrs, and
//     the summary line carries those too.
//
// After the handler returns, it writes the summary line with method, path,
// route (the ServeMux pattern from r.Pattern), status, bytes_out, duration,
// client_ip, query and user_agent. The level is Warn when the client
// cancelled the request, whatever the status, because the client went away
// rather than the service failing; Error for a 5xx; Info otherwise.
//
// query is a group with one attribute per parameter. The value of a parameter
// whose name secret.IsKey matches ("token", "api_key", "X-Amz-Signature", ...) is
// secret.Redacted, whatever logger writes the line. A parameter that does not parse
// is left out.
//
// A handler that panics still gets its summary line: at Error, with
// panic=<value> and status 500 unless the handler had already written a
// status. The middleware then panics again with the same value, so the
// server or an outer recovery middleware handles it as before; the stack it
// prints still starts at the original panic. http.ErrAbortHandler, which a
// handler raises on purpose to abort a response, is logged at Warn.
//
// A hijacked connection (a WebSocket upgrade) is logged with hijacked=true
// and status 101, unless the handler wrote a status before hijacking. Bytes
// written on the hijacked connection are not counted.
//
// Install it inside the tracing middleware, so the summary line carries the
// server span's trace_id, and outside the router, so it sees every request.
// The route comes from the *http.Request the router receives: a middleware
// between this one and the router that replaces the request (r.WithContext)
// hides it. A router that does not set r.Pattern can call
// SetAttrs(ctx, slog.String(KeyRoute, pattern)) instead.
//
// The request body is never logged: a body is where passwords and tokens
// travel, and key redaction cannot see inside it.
func HTTPMiddleware(opts HTTPOptions) func(http.Handler) http.Handler {
	trusted := opts.TrustedProxies
	if trusted == nil {
		trusted = defaultTrustedProxies
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			inner, recorder := openRequest(w, r)
			defer func() {
				if p := recover(); p != nil {
					recorder.panicked, recorder.panicValue = true, p
					defer panic(p) // after the summary line, with the original stack below
				}
				if !recorder.panicked && opts.SkipSummary != nil && opts.SkipSummary(inner) {
					return
				}
				logger := opts.Logger
				if logger == nil {
					logger = slog.Default()
				}
				writeSummary(logger, inner, recorder, start, trusted)
			}()
			next.ServeHTTP(recorder, inner)
		})
	}
}

// openRequest echoes or mints the request id, opens the request scope and
// wraps w. It returns the request the handler receives.
func openRequest(w http.ResponseWriter, r *http.Request) (*http.Request, *statusRecorder) {
	requestID := inboundRequestID(r.Header.Get(RequestIDHeader))
	if requestID == "" {
		requestID = uuid.NewV7().String()
	}
	w.Header().Set(RequestIDHeader, requestID)

	ctx := WithScope(r.Context())
	SetAttrs(ctx,
		slog.String(KeyRequestID, requestID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	return r.WithContext(ctx), &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// writeSummary writes the summary line of r, the request the handler
// received, with the scope attributes its context carries.
func writeSummary(
	logger *slog.Logger, r *http.Request, recorder *statusRecorder, start time.Time, trusted []netip.Prefix,
) {
	ctx := r.Context()
	status := recorder.finalStatus()
	level := summaryLevel(r, recorder, status)
	if !logger.Enabled(ctx, level) {
		return
	}
	attrs := make([]slog.Attr, 0, 12)
	if r.Pattern != "" {
		attrs = append(attrs, slog.String(KeyRoute, r.Pattern))
	}
	attrs = append(attrs,
		slog.Int(KeyStatus, status),
		slog.Int("bytes_out", recorder.bytes),
		slog.Duration("duration", time.Since(start)),
		slog.String("client_ip", clientIP(r, trusted)),
	)
	if r.URL.RawQuery != "" {
		attrs = append(attrs, queryAttr(r.URL.RawQuery))
	}
	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, slog.String("user_agent", ua))
	}
	if ctx.Err() != nil {
		attrs = append(attrs, slog.Bool("client_canceled", true))
	}
	if recorder.hijacked {
		attrs = append(attrs, slog.Bool("hijacked", true))
	}
	if recorder.panicked {
		attrs = append(attrs, slog.Any("panic", recorder.panicValue))
	}
	// pc 0: the source would always be this file, which says nothing.
	record := slog.NewRecord(time.Now(), level, "http request", 0)
	record.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, record) // like slog.Logger, a failed write is not the request's error
}

// queryAttr returns the query string as a group with one attribute per
// parameter, sorted by name: a string for one value, a list for several. The
// value of a parameter whose name secret.IsKey matches is secret.Redacted here, so the line
// is safe whatever handler writes it. A parameter that does not parse (bad
// percent-encoding) is left out; the others are kept. The raw query is never
// logged, since it would carry the secret values.
func queryAttr(rawQuery string) slog.Attr {
	values, _ := url.ParseQuery(rawQuery) // on error, the parameters that parsed are still worth logging
	attrs := make([]slog.Attr, 0, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		switch v := values[name]; {
		case secret.IsKey(name):
			attrs = append(attrs, slog.String(name, secret.Redacted))
		case len(v) == 1:
			attrs = append(attrs, slog.String(name, v[0]))
		default:
			attrs = append(attrs, slog.Any(name, v))
		}
	}
	return slog.Attr{Key: "query", Value: slog.GroupValue(attrs...)}
}

// summaryLevel picks the summary line's level. A panic is Error, except
// http.ErrAbortHandler, which aborts a response on purpose. A cancelled
// request context means the client disconnected; that is checked before the
// status, because a handler that gives up on a cancelled context often
// answers 5xx.
func summaryLevel(r *http.Request, recorder *statusRecorder, status int) slog.Level {
	switch {
	case recorder.panicked && recorder.panicValue == http.ErrAbortHandler: //nolint:errorlint // net/http compares it the same way
		return slog.LevelWarn
	case recorder.panicked:
		return slog.LevelError
	case r.Context().Err() != nil:
		return slog.LevelWarn
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// inboundRequestID returns id when it is a usable request id: 1 to 128
// printable ASCII characters. It returns "" otherwise.
func inboundRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLength {
		return ""
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return ""
		}
	}
	return id
}

// clientIP returns the address of the client that reached the first trusted
// proxy. When the connection comes from a trusted proxy, it walks
// X-Forwarded-For from the right, skipping trusted hops, and returns the first
// hop that is not trusted: every hop to its right was appended by a trusted
// proxy, while hops to its left are whatever the client sent. Without
// X-Forwarded-For it uses X-Real-Ip from the trusted proxy. Otherwise it
// returns the connection address, so a client that is not behind a trusted
// proxy cannot choose its client_ip.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		if host, _, splitErr := net.SplitHostPort(r.RemoteAddr); splitErr == nil {
			return host
		}
		return r.RemoteAddr
	}
	peer := remote.Addr().Unmap()
	if !isTrusted(peer, trusted) {
		return peer.String()
	}
	if hops := r.Header.Values("X-Forwarded-For"); len(hops) > 0 {
		return forwardedClient(strings.Join(hops, ","), peer, trusted)
	}
	if realIP, parseErr := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); parseErr == nil {
		return realIP.Unmap().String()
	}
	return peer.String()
}

// forwardedClient returns the rightmost hop of the X-Forwarded-For list that
// is not a trusted proxy. When every hop is trusted, the leftmost one is the
// client. A hop that does not parse ends the walk at the last address known to
// be good.
func forwardedClient(list string, peer netip.Addr, trusted []netip.Prefix) string {
	client := peer
	for _, raw := range slices.Backward(strings.Split(list, ",")) {
		hop, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err != nil {
			break
		}
		client = hop.Unmap()
		if !isTrusted(client, trusted) {
			break
		}
	}
	return client.String()
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(addr) })
}

// statusRecorder captures the status and the body size of a response, and
// whether the handler hijacked the connection or panicked.
type statusRecorder struct {
	http.ResponseWriter

	status      int
	bytes       int
	wroteHeader bool
	hijacked    bool
	panicked    bool
	panicValue  any
}

// finalStatus returns the status the summary line reports: 500 for a panic
// before any status was written, 101 for a hijack before any status was
// written, and the recorded status otherwise.
func (rec *statusRecorder) finalStatus() int {
	switch {
	case rec.wroteHeader:
		return rec.status
	case rec.panicked:
		return http.StatusInternalServerError
	case rec.hijacked:
		return http.StatusSwitchingProtocols
	default:
		return rec.status
	}
}

func (rec *statusRecorder) WriteHeader(code int) {
	// 1xx responses (103 Early Hints) precede the final status.
	if !rec.wroteHeader && code >= http.StatusOK {
		rec.status = code
		rec.wroteHeader = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	rec.wroteHeader = true
	n, err := rec.ResponseWriter.Write(b)
	rec.bytes += n
	return n, err
}

// ReadFrom forwards to the wrapped writer's io.ReaderFrom, so io.Copy from a
// file into the response keeps net/http's sendfile path, and counts the bytes.
func (rec *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	rec.wroteHeader = true
	var n int64
	var err error
	if rf, ok := rec.ResponseWriter.(io.ReaderFrom); ok {
		n, err = rf.ReadFrom(src)
	} else {
		n, err = io.Copy(writerOnly{rec.ResponseWriter}, src)
	}
	rec.bytes += int(n)
	return n, err
}

// writerOnly hides every method of a writer but Write, so io.Copy does not
// call ReadFrom on it again.
type writerOnly struct{ io.Writer }

// Flush forwards to the wrapped writer, through any Unwrap chain, so wrapping
// does not break SSE and other streaming handlers.
func (rec *statusRecorder) Flush() {
	rec.wroteHeader = true
	_ = http.NewResponseController(rec.ResponseWriter).Flush() // http.Flusher has no error result
}

// Hijack implements http.Hijacker by forwarding through any Unwrap chain, so
// WebSocket libraries that type-assert http.Hijacker work behind the
// middleware. It returns http.ErrNotSupported when the wrapped writer cannot
// hijack, as HTTP/2 writers cannot.
func (rec *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(rec.ResponseWriter).Hijack()
	if err == nil {
		rec.hijacked = true
	}
	return conn, rw, err
}

// Unwrap exposes the wrapped writer to http.ResponseController, so a handler
// can still clear its write deadline or flush with an error result.
func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}
