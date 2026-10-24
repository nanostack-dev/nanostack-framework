package log

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// Keys written by TraceEnricher. They match the OpenTelemetry log data model,
// so a log backend can link a line to its trace.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// TraceEnricher returns the Enricher that adds trace_id and span_id, as
// lowercase hex, when ctx carries a valid OpenTelemetry span context. Without
// one it adds nothing, so it is safe on every context.
//
// modules/logging registers it; a service wiring a Binder by hand passes it to
// NewBinder. Register it once: zerolog does not deduplicate keys.
func TraceEnricher() Enricher {
	return EnricherFunc(WithTrace)
}

// WithTrace adds the trace fields of ctx's span context to c. It is the
// function behind TraceEnricher, exported for middleware that builds a logger
// without a Binder.
func WithTrace(ctx context.Context, c zerolog.Context) zerolog.Context {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return c
	}
	return c.Str(TraceIDKey, spanContext.TraceID().String()).Str(SpanIDKey, spanContext.SpanID().String())
}
