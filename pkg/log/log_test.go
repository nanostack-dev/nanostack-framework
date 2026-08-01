package log_test

import (
	"context"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
)

// installForTest publishes binder as the process default and restores whatever
// was there before, so tests do not leak into each other.
func installForTest(t *testing.T, binder *log.Binder) {
	t.Helper()
	previous := log.Install(binder)
	t.Cleanup(func() { log.Install(previous) })
}

func TestBindAppliesEnrichersInOrder(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	binder := log.NewBinder(base,
		log.Static("component", "flow_schedule_fire"),
		log.EnricherFunc(func(ctx context.Context, c zerolog.Context) zerolog.Context {
			if org, ok := ctx.Value(orgKey{}).(string); ok {
				return c.Str("org_id", org)
			}
			return c
		}),
	)

	ctx := binder.Bind(context.WithValue(context.Background(), orgKey{}, "org_2xKqvB7nR4"))
	log.Ctx(ctx).Info().Msg("flow schedule fired")

	line := sink.decode(t)
	if line["component"] != "flow_schedule_fire" {
		t.Fatalf("component = %v", line["component"])
	}
	if line["org_id"] != "org_2xKqvB7nR4" {
		t.Fatalf("org_id = %v", line["org_id"])
	}
}

// An enricher whose source is absent must be a no-op, not an error: background
// paths legitimately have no auth context.
func TestBindSkipsEnrichersWithNothingToAdd(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	binder := log.NewBinder(base, log.EnricherFunc(
		func(ctx context.Context, c zerolog.Context) zerolog.Context {
			if org, ok := ctx.Value(orgKey{}).(string); ok {
				return c.Str("org_id", org)
			}
			return c
		},
	))

	ctx := binder.Bind(context.Background())
	log.Ctx(ctx).Info().Msg("no org here")

	line := sink.decode(t)
	if _, ok := line["org_id"]; ok {
		t.Fatalf("expected no org_id, got %v", line)
	}
}

// Re-binding is the documented escape hatch for values that land after the
// first Bind — a cron resolving each row's tenant mid-tick.
func TestRebindAddsLateArrivingFields(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	binder := log.NewBinder(base, log.EnricherFunc(
		func(ctx context.Context, c zerolog.Context) zerolog.Context {
			if org, ok := ctx.Value(orgKey{}).(string); ok {
				return c.Str("org_id", org)
			}
			return c
		},
	))

	ctx := binder.Bind(context.Background())
	ctx = binder.Bind(context.WithValue(ctx, orgKey{}, "org_late"))

	log.Ctx(ctx).Info().Msg("fired")

	if line := sink.decode(t); line["org_id"] != "org_late" {
		t.Fatalf("org_id = %v, want org_late", line["org_id"])
	}
}

// The whole point of the floor: a context nobody bound must still log, so a
// missed Bind degrades to a line without ambient fields instead of silence.
func TestCtxFallsBackToBaseWhenUnbound(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel).With().Str("service", "echopoint").Logger()
	installForTest(t, log.NewBinder(base))

	log.Ctx(context.Background()).Info().Msg("unbound but not lost")

	line := sink.decode(t)
	if line["service"] != "echopoint" {
		t.Fatalf("expected the base logger, got %v", line)
	}
	if line["message"] != "unbound but not lost" {
		t.Fatalf("message = %v", line["message"])
	}
}

func TestCtxPrefersBoundLoggerOverBase(t *testing.T) {
	var baseSink, boundSink capture
	installForTest(t, log.NewBinder(zerolog.New(&baseSink).Level(zerolog.DebugLevel)))

	bound := zerolog.New(&boundSink).Level(zerolog.DebugLevel).With().Str("request_id", "req_1").Logger()
	ctx := bound.WithContext(context.Background())

	log.Ctx(ctx).Info().Msg("hello")

	if len(baseSink.last) != 0 {
		t.Fatal("a bound context must not fall through to the base logger")
	}
	if line := boundSink.decode(t); line["request_id"] != "req_1" {
		t.Fatalf("request_id = %v", line["request_id"])
	}
}

func TestPackageBindWithoutInstallIsInert(t *testing.T) {
	installForTest(t, nil)

	ctx := log.Bind(context.Background())
	if ctx == nil {
		t.Fatal("Bind must return a usable context even with nothing installed")
	}
	// Must not panic.
	log.Ctx(ctx).Info().Msg("discarded")
}

func TestOpUsesCallerName(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	installForTest(t, log.NewBinder(base))

	(&repo{}).GetByID(context.Background())

	if line := sink.decode(t); line["operation"] != "GetByID" {
		t.Fatalf("operation = %v, want GetByID", line["operation"])
	}
}

// Documents the known footgun rather than pretending it away: inside a closure
// Op reports the literal, so OpNamed is the escape hatch.
func TestOpInsideClosureReportsTheClosure(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	installForTest(t, log.NewBinder(base))

	func() {
		log.Op(context.Background()).Info().Msg("inside a closure")
	}()

	if line := sink.decode(t); line["operation"] == "TestOpInsideClosureReportsTheClosure" {
		t.Fatal("Op is expected to report the closure, not the enclosing test; use OpNamed there")
	}
}

func TestOpNamedOverridesTheFrame(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	installForTest(t, log.NewBinder(base))

	func() {
		log.OpNamed(context.Background(), "fireSchedule").Info().Msg("inside a closure")
	}()

	if line := sink.decode(t); line["operation"] != "fireSchedule" {
		t.Fatalf("operation = %v, want fireSchedule", line["operation"])
	}
}

type orgKey struct{}

type repo struct{}

func (r *repo) GetByID(ctx context.Context) {
	log.Op(ctx).Info().Msg("loaded")
}
