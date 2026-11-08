package tracing_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/tracing"
)

func TestLogAttrsPutTraceIDsOnOutputLines(t *testing.T) {
	t.Parallel()
	traceID := trace.TraceID{
		0x0a,
		0xf7,
		0x65,
		0x19,
		0x16,
		0xcd,
		0x43,
		0xdd,
		0x84,
		0x48,
		0xeb,
		0x21,
		0x1c,
		0x80,
		0x31,
		0x9c,
	}
	spanID := trace.SpanID{0xb7, 0xad, 0x6b, 0x71, 0x69, 0x20, 0x33, 0x31}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	var out bytes.Buffer
	logger := log.New(log.Config{Output: &out, ContextAttrs: tracing.LogAttrs})

	logger.InfoContext(ctx, "traced")
	logger.InfoContext(t.Context(), "untraced")

	dec := json.NewDecoder(&out)
	var traced, untraced map[string]any
	if err := dec.Decode(&traced); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&untraced); err != nil {
		t.Fatal(err)
	}
	if traced[log.KeyTraceID] != "0af7651916cd43dd8448eb211c80319c" || traced[log.KeySpanID] != "b7ad6b7169203331" {
		t.Fatalf("traced line = %v", traced)
	}
	if _, ok := untraced[log.KeyTraceID]; ok {
		t.Fatalf("untraced line = %v", untraced)
	}
	if attrs := tracing.LogAttrs(t.Context()); attrs != nil {
		t.Fatalf("LogAttrs without a span = %v", attrs)
	}
}
