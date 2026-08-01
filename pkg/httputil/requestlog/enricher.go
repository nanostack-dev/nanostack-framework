package requestlog

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
)

// NewLogEnricher returns the log.Enricher that republishes the request identity
// Contextualize established.
//
// Register it alongside the application's own enrichers so that a logger
// rebuilt by log.Bind carries request_id, method and path. Without it, binding
// inside a request would produce a logger with the application's fields but
// none of the request's.
//
// Off the HTTP path every value is absent and the enricher is a no-op, which is
// what makes it safe to register unconditionally.
func NewLogEnricher() log.Enricher {
	return log.EnricherFunc(func(ctx context.Context, c zerolog.Context) zerolog.Context {
		if requestID := RequestIDFromContext(ctx); requestID != "" {
			c = c.Str("request_id", requestID)
		}
		if method := MethodFromContext(ctx); method != "" {
			c = c.Str("method", method)
		}
		if path := PathFromContext(ctx); path != "" {
			c = c.Str("path", path)
		}
		return c
	})
}
