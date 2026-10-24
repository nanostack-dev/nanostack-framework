package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

func spanContext() context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	}))
}

func TestTraceEnricherAddsHexIDs(t *testing.T) {
	var out bytes.Buffer
	binder := log.NewBinder(zerolog.New(&out), log.TraceEnricher())

	log.Ctx(binder.Bind(spanContext())).Info().Msg("job ran")

	var line map[string]any
	if err := json.Unmarshal(out.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line[log.TraceIDKey] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %v", line[log.TraceIDKey])
	}
	if line[log.SpanIDKey] != "00f067aa0ba902b7" {
		t.Fatalf("span_id = %v", line[log.SpanIDKey])
	}
}

func TestTraceEnricherNoOpWithoutSpan(t *testing.T) {
	var out bytes.Buffer
	binder := log.NewBinder(zerolog.New(&out), log.TraceEnricher())

	log.Ctx(binder.Bind(context.Background())).Info().Msg("cron tick")

	if strings.Contains(out.String(), log.TraceIDKey) || strings.Contains(out.String(), log.SpanIDKey) {
		t.Fatalf("expected no trace fields, got %s", out.String())
	}
}

func TestTraceEnricherRebindDoesNotDuplicate(t *testing.T) {
	var out bytes.Buffer
	binder := log.NewBinder(zerolog.New(&out), log.TraceEnricher())

	log.Ctx(binder.Bind(binder.Bind(spanContext()))).Info().Msg("handled")

	for _, key := range []string{log.TraceIDKey, log.SpanIDKey} {
		if count := strings.Count(out.String(), `"`+key+`"`); count != 1 {
			t.Fatalf("%s appeared %d times in %s", key, count, out.String())
		}
	}
}
