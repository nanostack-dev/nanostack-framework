package requestlog

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
)

const defaultMaxBodySize = 1024 * 4

type statusRecorder struct {
	http.ResponseWriter

	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
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
			logRequest(summaryLogger, r, recorder.statusCode, start, requestBody, opts.LogRequestBody, withRoute)
			logServerError(summaryLogger, r, recorder.statusCode, start, withRoute)
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

func logRequest(
	log zerolog.Logger,
	r *http.Request,
	statusCode int,
	start time.Time,
	requestBody string,
	logRequestBody bool,
	withRoute bool,
) {
	entry := log.Info()
	if withRoute {
		entry = entry.Str("method", r.Method).Str("path", r.URL.Path)
	}
	duration := time.Since(start)
	entry = entry.
		Int("status", statusCode).
		Dur("duration", duration)
	if logRequestBody && requestBody != "" && requestBody != "Error reading request body" {
		entry = entry.Str("request_body", requestBody)
	}
	entry.Msg(requestSummary(r, statusCode, duration))
}

// requestSummary builds a compact, human-scannable headline such as
// "GET /flows/123 -> 200 (2.5ms)". The structured fields (request_id, org_id,
// status, duration, ...) still live on the log entry; this is only the message.
func requestSummary(r *http.Request, statusCode int, duration time.Duration) string {
	return fmt.Sprintf("%s %s -> %d (%s)", r.Method, r.URL.Path, statusCode, duration.Round(time.Microsecond))
}

func logServerError(log zerolog.Logger, r *http.Request, statusCode int, start time.Time, withRoute bool) {
	if statusCode < http.StatusInternalServerError || statusCode > 599 {
		return
	}
	entry := log.Error()
	if withRoute {
		entry = entry.Str("method", r.Method).Str("path", r.URL.Path)
	}
	duration := time.Since(start)
	entry.
		Int("status", statusCode).
		Dur("duration", duration).
		Msg(requestSummary(r, statusCode, duration))
}
