package requestlog

import (
	"bytes"
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
			start := time.Now()
			requestBody := requestBodyForLogging(r, log, opts)
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)
			logRequest(log, r, recorder.statusCode, start, requestBody, opts.LogRequestBody)
			logServerError(log, r, recorder.statusCode, start)
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
) {
	entry := log.Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("status", statusCode).
		Dur("duration", time.Since(start))
	if logRequestBody && requestBody != "" && requestBody != "Error reading request body" {
		entry = entry.Str("request_body", requestBody)
	}
	entry.Msg("incoming request")
}

func logServerError(log zerolog.Logger, r *http.Request, statusCode int, start time.Time) {
	if statusCode < http.StatusInternalServerError || statusCode > 599 {
		return
	}
	log.Error().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("status", statusCode).
		Dur("duration", time.Since(start)).
		Msgf("server error: %s", http.StatusText(statusCode))
}
