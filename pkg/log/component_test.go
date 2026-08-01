package log_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/log"
	"github.com/rs/zerolog"
)

func TestComponentIsBoundWithoutWiring(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	// No enrichers passed: ComponentEnricher must still apply.
	ctx := log.NewBinder(base).Bind(log.WithComponent(context.Background(), "flow_schedule_fire"))
	log.Ctx(ctx).Info().Msg("cron tick")

	if line := sink.decode(t); line["component"] != "flow_schedule_fire" {
		t.Fatalf("component = %v, want flow_schedule_fire", line["component"])
	}
}

func TestComponentAbsentOnRequestPath(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)

	ctx := log.NewBinder(base).Bind(context.Background())
	log.Ctx(ctx).Info().Msg("request")

	if line := sink.decode(t); line["component"] != nil {
		t.Fatalf("expected no component, got %v", line["component"])
	}
}

// The component must survive a re-bind without doubling, since background
// entry points re-bind per row.
func TestComponentSurvivesRebindWithoutDuplicating(t *testing.T) {
	var sink capture
	base := zerolog.New(&sink).Level(zerolog.DebugLevel)
	binder := log.NewBinder(base)

	ctx := binder.Bind(log.WithComponent(context.Background(), "search_sync"))
	ctx = binder.Bind(ctx)
	log.Ctx(ctx).Info().Msg("tick")

	if count := strings.Count(string(sink.last), `"component"`); count != 1 {
		t.Fatalf("component appeared %d times in %s, want exactly 1", count, sink.last)
	}
}

func TestWithComponentIgnoresEmptyName(t *testing.T) {
	ctx := log.WithComponent(context.Background(), "")
	if got := log.ComponentFromContext(ctx); got != "" {
		t.Fatalf("ComponentFromContext = %q, want empty", got)
	}
}
