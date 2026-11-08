package nano

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// With DrainDelay, readiness turns unavailable while the Runners still
// serve, so a load balancer stops sending traffic before the listeners
// close.
func TestDrainDelayKeepsRunnersServingWhileNotReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{DrainDelay: 5 * time.Second, StopTimeout: 15 * time.Second})
		runnerStopped := make(chan time.Time, 1)
		app.OnRun("http", func(ctx context.Context) error {
			<-ctx.Done()
			runnerStopped <- time.Now()
			return nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- app.Run(ctx) }()
		synctest.Wait()
		if err := app.Ready(t.Context()); err != nil {
			t.Fatalf("Ready before the stop = %v", err)
		}

		began := time.Now()
		cancel() // stands in for SIGTERM
		time.Sleep(time.Second)
		synctest.Wait()
		if app.Ready(t.Context()) == nil {
			t.Fatal("the App is ready during the drain delay")
		}
		select {
		case <-runnerStopped:
			t.Fatal("the Runner was cancelled before the drain delay passed")
		default:
		}
		if err := <-done; err != nil {
			t.Fatalf("Run = %v", err)
		}
		if at := (<-runnerStopped).Sub(began); at != 5*time.Second {
			t.Fatalf("the Runner was cancelled %v after the stop, want the 5s drain delay", at)
		}
	})
}

// An App that never ran has no traffic to drain: Stop does not wait.
func TestDrainDelaySkippedWhenNotRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{DrainDelay: 5 * time.Second})
		began := time.Now()
		if err := app.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(began); took != 0 {
			t.Fatalf("Stop of an App that never ran took %v", took)
		}
	})
}

// A DrainDelay that leaves the Runners less than StopGrace of StopTimeout
// fails Start before any component starts, and Stop releases what the
// constructors acquired.
func TestStartRejectsADrainDelayWithoutARunnerBudget(t *testing.T) {
	for _, cfg := range []AppConfig{
		{DrainDelay: 15 * time.Second, StopTimeout: 15 * time.Second},
		{DrainDelay: 20 * time.Second, StopTimeout: 15 * time.Second},
		{DrainDelay: 14*time.Second + time.Millisecond, StopTimeout: 15 * time.Second},
	} {
		app := newTestApp(cfg)
		var rec recorder
		app.OnStop("db", rec.fn("stop db", nil))
		app.Hook("http", rec.fn("start http", nil), rec.fn("stop http", nil))

		err := app.Start(t.Context())

		if err == nil || !strings.Contains(err.Error(), "drain_delay") {
			t.Fatalf("Start with %+v = %v, want a drain_delay error", cfg, err)
		}
		assertEvents(t, rec.get(), []string{"stop db"})
	}
}

// StopGrace left for the Runners is enough.
func TestStartAcceptsADrainDelayThatLeavesStopGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{DrainDelay: 14 * time.Second, StopTimeout: 15 * time.Second})
		if err := app.Start(t.Context()); err != nil {
			t.Fatalf("Start = %v", err)
		}
		if err := app.Stop(t.Context()); err != nil {
			t.Fatalf("Stop = %v", err)
		}
	})
}

// A caller that cancels the Stop context during the drain ends the wait:
// the Runners are cancelled then, not after the whole DrainDelay.
func TestDrainEndsWhenTheStopContextIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{DrainDelay: 5 * time.Second, StopTimeout: 15 * time.Second})
		runnerStopped := make(chan time.Time, 1)
		app.OnRun("http", func(ctx context.Context) error {
			<-ctx.Done()
			runnerStopped <- time.Now()
			return nil
		})
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		began := time.Now()
		if err := app.Stop(stopCtx); err != nil {
			t.Fatalf("Stop = %v", err)
		}

		if at := (<-runnerStopped).Sub(began); at != time.Second {
			t.Fatalf("the Runner was cancelled %v after the stop, want 1s, when the stop context ended", at)
		}
	})
}
