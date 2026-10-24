package logging_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/modules/logging"
	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

func TestModuleRegistersTraceEnricher(t *testing.T) {
	defer log.Install(log.Install(nil))
	var out bytes.Buffer
	var binder *log.Binder
	app := fx.New(
		fx.NopLogger,
		logging.Module,
		fx.Decorate(func(zerolog.Logger) zerolog.Logger { return zerolog.New(&out) }),
		fx.Populate(&binder),
	)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	}))
	log.Ctx(binder.Bind(ctx)).Info().Msg("job")
	if count := strings.Count(out.String(), `"trace_id":"01000000000000000000000000000000"`); count != 1 {
		t.Fatalf("trace_id appeared %d times in %s", count, out.String())
	}
}
