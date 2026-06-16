package requestlog

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const defaultMaxBodySize = 1024 * 4

type statusRecorder struct {
	http.ResponseWriter

	statusCode   int
	bytesWritten int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(b)
	rec.bytesWritten += n
	return n, err
}

// Flush propagates flushes (used by SSE / streaming handlers) to the wrapped
// writer when it supports them, so wrapping for audit logging does not disable
// streaming.
func (rec *statusRecorder) Flush() {
	if flusher, ok := rec.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Options controls request logging behavior.
type Options struct {
	LogRequestBody bool
	MaxBodySize    int
	Skip           func(*http.Request) bool
}

// New creates HTTP request logging middleware.
func New(log zerolog.Logger, opts Options) func(http.Handler) http.Handler {
	if opts.MaxBodySize <= 0 {
		opts.MaxBodySize = defaultMaxBodySize
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.Skip != nil && opts.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			// When Contextualize ran first, reuse the request-scoped logger
			// (already carrying request_id, method and path) and drop the route
			// fields here to avoid duplicate keys. Standalone consumers without
			// Contextualize keep the original behavior via the fallback logger.
			//
			// The context logger is held as a pointer and only dereferenced when
			// the summary line is written, after next has run. Inner middleware
			// (for example auth resolving an org id) enriches the same logger in
			// place via UpdateContext, so those fields land on the summary line.
			ctxLogger := From(r.Context())
			useCtx := hasContextLogger(r)
			bodyLogger := log
			if useCtx {
				bodyLogger = *ctxLogger
			}
			start := time.Now()
			requestBody := requestBodyForLogging(r, bodyLogger, opts)
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)
			summaryLogger := log
			withRoute := true
			if useCtx {
				summaryLogger = *ctxLogger
				withRoute = false
			}
			logRequest(summaryLogger, r, recorder, start, requestBody, opts.LogRequestBody, withRoute)
		})
	}
}

// NewFromEnv creates middleware using LOG_REQUEST_BODY=true as the body logging switch.
func NewFromEnv(log zerolog.Logger) func(http.Handler) http.Handler {
	return New(log, Options{LogRequestBody: os.Getenv("LOG_REQUEST_BODY") == "true"})
}

func requestBodyForLogging(r *http.Request, log zerolog.Logger, opts Options) string {
	if !opts.LogRequestBody || r.Body == nil {
		return ""
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("failed to read request body")
		return "Error reading request body"
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	requestBody := string(bodyBytes)
	if len(requestBody) > opts.MaxBodySize {
		requestBody = requestBody[:opts.MaxBodySize] + "...(truncated)"
	}
	return requestBody
}

// logRequest emits the single request/response audit line for a completed
// request: one structured entry, written after the handler returns, carrying
// the request shape (method, path, query, client ip, user agent), the response
// outcome (status, bytes) and timing (duration). It inherits request_id and any
// auth fields (org_id, user_id, ...) from the request-scoped logger. 5xx
// responses are logged at error level; everything else at info.
func logRequest(
	log zerolog.Logger,
	r *http.Request,
	recorder *statusRecorder,
	start time.Time,
	requestBody string,
	logRequestBody bool,
	withRoute bool,
) {
	duration := time.Since(start)
	statusCode := recorder.statusCode

	entry := log.Info()
	if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
		entry = log.Error()
	}
	if withRoute {
		entry = entry.Str("method", r.Method).Str("path", r.URL.Path)
	}
	entry = entry.
		Int("status", statusCode).
		Int("bytes_out", recorder.bytesWritten).
		Dur("duration", duration).
		Str("client_ip", clientIP(r))
	if query := r.URL.RawQuery; query != "" {
		entry = entry.Str("query", query)
	}
	if userAgent := r.UserAgent(); userAgent != "" {
		entry = entry.Str("user_agent", userAgent)
	}
	if logRequestBody && requestBody != "" && requestBody != "Error reading request body" {
		entry = entry.Str("request_body", requestBody)
	}
	entry.Msg(requestSummary(r, statusCode, duration))
}

// requestSummary builds a compact, human-scannable headline such as
// "GET /flows/123 -> 200 (2.5ms)". The structured fields (request_id, org_id,
// status, bytes_out, duration, ...) still live on the log entry; this is only
// the message.
func requestSummary(r *http.Request, statusCode int, duration time.Duration) string {
	return fmt.Sprintf("%s %s -> %d (%s)", r.Method, r.URL.Path, statusCode, duration.Round(time.Microsecond))
}

// clientIP returns the originating client address, preferring the first hop in
// X-Forwarded-For, then X-Real-Ip, then the connection's RemoteAddr (with the
// port stripped).
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		return realIP
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
