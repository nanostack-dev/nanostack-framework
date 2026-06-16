package requestlog

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// RequestIDHeader is the canonical header carrying the per-request correlation
// id, both inbound and on the response.
const RequestIDHeader = "X-Request-Id"

type contextKey int

const requestIDContextKey contextKey = iota

// Contextualize returns middleware that establishes a per-request correlation
// id and a request-scoped zerolog logger on the context.
//
// For each request it:
//   - reuses an inbound X-Request-Id header when present, otherwise mints a
//     UUIDv7 (time-ordered) id;
//   - stores the id on the context (see RequestIDFromContext) and echoes it
//     back on the X-Request-Id response header;
//   - derives a child logger from base carrying request_id, method and path and
//     stores it on the context, so downstream code can retrieve it with From
//     (or zerolog.Ctx) and attach further fields in place via
//     (*zerolog.Logger).UpdateContext.
//
// It must run before any middleware that enriches the request logger (for
// example an auth middleware adding an org id) and before New, which reuses the
// context logger when present.
func Contextualize(base zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(RequestIDHeader)
			if requestID == "" {
				requestID = uuid.Must(uuid.NewV7()).String()
			}
			w.Header().Set(RequestIDHeader, requestID)

			logger := base.With().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Logger()

			ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
			ctx = logger.WithContext(ctx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// From returns the request-scoped logger stored on ctx by Contextualize. When
// no logger is present it returns a disabled logger, so callers may log
// unconditionally without a nil check.
func From(ctx context.Context) *zerolog.Logger {
	return zerolog.Ctx(ctx)
}

// RequestIDFromContext returns the correlation id stored by Contextualize, or an
// empty string when Contextualize did not run for this request.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey).(string); ok {
		return id
	}
	return ""
}

// hasContextLogger reports whether Contextualize ran for r, meaning a
// request-scoped logger (already carrying request_id, method and path) is
// present on the context.
func hasContextLogger(r *http.Request) bool {
	return RequestIDFromContext(r.Context()) != ""
}
