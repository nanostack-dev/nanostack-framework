package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// LogAttrs returns trace_id and span_id, as lowercase hex, when ctx carries a
// valid OpenTelemetry span context, and nil otherwise. The keys match the
// OpenTelemetry log data model, so a log backend links the line to its trace.
//
// Pass it to core/log, which does not import OpenTelemetry itself:
//
//	cfg.ContextAttrs = tracing.LogAttrs
//	slog.SetDefault(log.New(cfg))
func LogAttrs(ctx context.Context) []slog.Attr {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return nil
	}
	return []slog.Attr{
		slog.String(log.KeyTraceID, spanContext.TraceID().String()),
		slog.String(log.KeySpanID, spanContext.SpanID().String()),
	}
}
