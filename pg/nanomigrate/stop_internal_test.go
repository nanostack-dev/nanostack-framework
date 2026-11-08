package nanomigrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
)

// TestStopOutcome checks what Migrate returns when its context ends while Up
// runs. Up returns nil after a graceful stop too, so only a pending migration
// makes the run a stop, and a failed migration stays a failure.
func TestStopOutcome(t *testing.T) {
	t.Parallel()
	cause := context.Canceled
	failed := errors.New("pq: syntax error")
	readErr := errors.New("read the source")
	tests := []struct {
		name        string
		upErr       error
		pending     bool
		pendingErr  error
		wantNil     bool
		wantStopped bool
		wantIs      error
	}{
		{name: "stopped before the last migration", pending: true, wantStopped: true},
		{name: "every migration ran before the stop", pending: false, wantNil: true},
		{name: "nothing to do", upErr: migrate.ErrNoChange, pending: false, wantNil: true},
		{name: "a migration failed", upErr: failed, pending: true, wantIs: failed},
		{name: "pending cannot be read", pendingErr: readErr, wantIs: readErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			pending := func() (bool, error) {
				called = true
				return tt.pending, tt.pendingErr
			}
			err := stopOutcome(tt.upErr, pending, cause)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("stopOutcome = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("stopOutcome = nil, want an error")
			}
			if got := errors.Is(err, ErrStopped); got != tt.wantStopped {
				t.Fatalf("errors.Is(%v, ErrStopped) = %v, want %v", err, got, tt.wantStopped)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("stopOutcome = %v, want it to wrap %v", err, tt.wantIs)
			}
			if !tt.wantStopped && errors.Is(err, context.Canceled) {
				t.Fatalf("stopOutcome = %v wraps context.Canceled: a failure must not read as a clean shutdown", err)
			}
			if tt.upErr != nil && !errors.Is(tt.upErr, migrate.ErrNoChange) && called {
				t.Fatal("stopOutcome read the pending migrations after a failure")
			}
		})
	}
}

// TestRunUntilStoppedReportsAStopThatEndsUpBeforeStopReturns checks that a
// stop is reported also when up returns before stop does. golang-migrate
// reads GracefulStop at once and Up then returns nil, so a check made before
// the watcher ends could report a stopped run as complete.
func TestRunUntilStoppedReportsAStopThatEndsUpBeforeStopReturns(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	signal := make(chan struct{}, 1)
	up := func() error {
		cancel()
		<-signal
		return nil
	}
	stop := func() {
		signal <- struct{}{}
		// Hold stop open after up can return: this is the gap the check must
		// not fall into.
		time.Sleep(20 * time.Millisecond)
	}
	stopped, err := runUntilStopped(ctx, up, stop)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !stopped {
		t.Fatal("stopped = false, want true: the stop was not reported")
	}
}

func TestRunUntilStoppedWithoutAStop(t *testing.T) {
	t.Parallel()
	failed := errors.New("pq: syntax error")
	stopped, err := runUntilStopped(t.Context(), func() error { return failed }, func() {
		t.Error("stop called without a cancelled context")
	})
	if !errors.Is(err, failed) || stopped {
		t.Fatalf("runUntilStopped = (%v, %v), want (false, %v)", stopped, err, failed)
	}
}
