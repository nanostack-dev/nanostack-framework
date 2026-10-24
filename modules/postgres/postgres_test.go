package postgres

import (
	"context"
	"testing"

	"github.com/XSAM/otelsql"
	"go.opentelemetry.io/otel/trace"
)

func TestQueriesTraceOnlyInsideAnExistingTrace(t *testing.T) {
	if hasParentSpan(context.Background(), otelsql.MethodConnQuery, "select 1", nil) {
		t.Fatal("a query without a parent span must not start a trace")
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled,
	}))
	if !hasParentSpan(ctx, otelsql.MethodConnQuery, "select 1", nil) {
		t.Fatal("a query inside a trace must produce a child span")
	}
}
