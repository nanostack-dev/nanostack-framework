package nano

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// assertErr fails unless err's message is exactly want.
func assertErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error:\n got  %v\n want %s", err, want)
	}
}

func TestStopDeadlineIsSharedAndEveryStopperIsCalled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		release := make(chan struct{})
		app := newTestApp(AppConfig{StopTimeout: 5 * time.Second})
		app.OnStop("first", rec.fn("stop first", nil))
		app.OnStop("stuck", func(context.Context) error {
			rec.add("stop stuck")
			<-release // ignores ctx
			return nil
		})
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}

		began := time.Now()
		err := app.Stop(t.Context())
		// "first" runs after the shared deadline, under StopGrace, and returns
		// at once: it is not reported, and it finished before Stop returned.
		assertErr(t, err, "nano: stop stuck: did not return in time: context deadline exceeded")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want it to wrap context.DeadlineExceeded", err)
		}
		if took := time.Since(began); took != 5*time.Second {
			t.Fatalf("Stop took %v, want the 5s stop timeout", took)
		}
		assertEvents(t, rec.get(), []string{"stop stuck", "stop first"})

		if again := app.Stop(t.Context()); again == nil || again.Error() != err.Error() {
			t.Fatalf("second Stop = %v, want the first result", again)
		}
		close(release)
	})
}

func TestRunnerIgnoringCancellationHitsStopDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		release := make(chan struct{})
		app := newTestApp(AppConfig{StopTimeout: 2 * time.Second})
		app.OnStop("pool", rec.fn("stop pool", nil))
		app.OnRun("deaf", func(context.Context) error { <-release; return nil })

		ctx, cancel := context.WithCancel(t.Context())
		errc := make(chan error, 1)
		go func() { errc <- app.Run(ctx) }()
		synctest.Wait()
		cancel()
		began := time.Now()
		err := <-errc
		// The pool stopper runs after the drain used the whole budget; it gets
		// StopGrace, returns at once, and is not reported.
		assertErr(t, err, "nano: runners [deaf] did not return before the stop deadline: context deadline exceeded")
		if took := time.Since(began); took != 2*time.Second {
			t.Fatalf("shutdown took %v, want the 2s stop timeout", took)
		}
		assertEvents(t, rec.get(), []string{"stop pool"}) // finished before Run returned
		close(release)
	})
}

// TestStoppersAfterTheDeadlineRunInOrderAndFinish is the regression test for
// stoppers that used to start in the background, all at once, and be
// reported as late once a slow Runner had used up StopTimeout.
func TestStoppersAfterTheDeadlineRunInOrderAndFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		release := make(chan struct{})
		app := newTestApp(AppConfig{StopTimeout: 2 * time.Second})
		app.OnStop("instant", rec.fn("stop instant", nil))
		app.OnStop("flush", func(ctx context.Context) error { // needs half of StopGrace
			select {
			case <-time.After(StopGrace / 2):
				rec.add("flush done")
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		app.OnStop("hung", func(context.Context) error { // ignores ctx
			rec.add("stop hung")
			<-release
			return nil
		})
		app.OnStop("pool", rec.fn("stop pool", nil))
		app.OnRun("deaf", func(context.Context) error { <-release; return nil })

		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		began := time.Now()
		err := app.Stop(t.Context())
		assertErr(t, err, strings.Join([]string{
			"nano: runners [deaf] did not return before the stop deadline: context deadline exceeded",
			"nano: stop hung: did not return in time: context deadline exceeded",
		}, "\n"))
		// Reverse order, one at a time, each finished when Stop returns.
		assertEvents(t, rec.get(), []string{"stop pool", "stop hung", "flush done", "stop instant"})
		// 2s drain, StopGrace for the hung stopper, half of it for the flush.
		if took, want := time.Since(began), 2*time.Second+StopGrace+StopGrace/2; took != want {
			t.Fatalf("Stop took %v, want %v", took, want)
		}
		close(release)
	})
}

func TestCallWordsItsErrorByCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		block := func(context.Context) error { <-release; return nil } // ignores ctx
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		assertErr(t, call(ctx, block), "did not return in time: context deadline exceeded")

		ctx, cancel = context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel)
		assertErr(t, call(ctx, block), "interrupted: context canceled")
		close(release)
		synctest.Wait()
	})
}

func TestStopBeforeStartReleasesConstructedResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{})
		app.OnStop("pool", rec.fn("stop pool", nil))
		app.OnCleanup("file", func() { rec.add("cleanup file") })
		app.Hook("cache", rec.fn("start cache", nil), rec.fn("stop cache", nil))
		if err := app.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertEvents(t, rec.get(), []string{"cleanup file", "stop pool"})
		if err := app.Start(t.Context()); err == nil {
			t.Fatal("Start after Stop succeeded")
		}
	})
}

func TestStopJoinsErrorsAndContinues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		e1, e2 := errors.New("e1"), errors.New("e2")
		app := newTestApp(AppConfig{})
		app.OnStop("a", rec.fn("stop a", e1))
		app.OnStop("b", rec.fn("stop b", nil))
		app.OnStop("c", rec.fn("stop c", e2))
		err := app.Stop(t.Context())
		if !errors.Is(err, e1) || !errors.Is(err, e2) {
			t.Fatalf("Stop = %v, want both errors joined", err)
		}
		assertEvents(t, rec.get(), []string{"stop c", "stop b", "stop a"})
	})
}
