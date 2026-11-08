package fault_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
)

func TestLevelForReadsProblemStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want slog.Level
	}{
		{"not found", fault.NotFound("FLOW_NOT_FOUND", "no flow"), slog.LevelDebug},
		{"bad request", fault.BadRequest("BAD", "bad"), slog.LevelDebug},
		{"internal", fault.Internal("DB", "db down"), slog.LevelError},
		{"canceled inside", fault.Internal("DB", "gave up").Wrap(context.Canceled), slog.LevelWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := log.LevelFor(tt.err); got != tt.want {
				t.Fatalf("LevelFor = %v, want %v", got, tt.want)
			}
		})
	}
}
