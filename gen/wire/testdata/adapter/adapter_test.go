package adapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/adapter/internal/gen/wire"
)

func TestAdapterRunsAndStopsTheComponentOnce(t *testing.T) {
	app, c, err := wire.BuildComponents(t.Context(), wire.Config{}, wire.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v; a second Run of the same server would fail or double-serve", err)
	}
	if runs, stops := c.Server.Runs.Load(), c.Server.Stops.Load(); runs != 1 || stops != 1 {
		t.Fatalf("Server.Run called %d times, Server.Stop %d times; want 1 and 1", runs, stops)
	}
}
