package nano

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

// A Starter that a shutdown interrupts is waited for before Stop runs, so
// its dependencies are not stopped under it, and a Starter that still
// succeeds is stopped in reverse order like any other.
func TestCancelledStarterIsWaitedForAndStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{StartTimeout: time.Minute})
		app.OnStop("pool", rec.fn("stop pool", nil))
		app.Hook("worker", func(ctx context.Context) error {
			rec.add("start worker begins")
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond) // unwinds, and holds its resource
			rec.add("start worker returns nil")
			return nil
		}, rec.fn("stop worker", nil))
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(20*time.Millisecond, cancel)

		err := app.Start(ctx)
		if !errors.Is(err, ErrShutdownDuringStart) {
			t.Fatalf("Start = %v, want ErrShutdownDuringStart", err)
		}
		assertEvents(t, rec.get(), []string{
			"start worker begins", "start worker returns nil", "stop worker", "stop pool",
		})
	})
}

// A Starter that outlives StartTimeout is abandoned, but not forgotten: when
// it succeeds later, a watcher stops it.
func TestAbandonedStarterIsStoppedWhenItSucceedsLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{StartTimeout: time.Second})
		app.Hook("slow", func(context.Context) error {
			time.Sleep(3 * time.Second) // ignores ctx
			rec.add("start slow returns nil")
			return nil
		}, rec.fn("stop slow", nil))

		if err := app.Start(t.Context()); err == nil {
			t.Fatal("Start passed although the Starter overran StartTimeout")
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		assertEvents(t, rec.get(), []string{"start slow returns nil", "stop slow"})
	})
}
