package nano

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	nanolog "github.com/nanostack-dev/nanostack-framework/core/log"
)

// StopGrace is the least time each Stopper gets. A Stopper called after
// StopTimeout has passed (because the Runners drained slowly, or an earlier
// Stopper overran) still runs, alone and in order, under a deadline
// StopGrace from its call. So Stop returns within StopTimeout, plus at most
// StopGrace for each Stopper called after that.
const StopGrace = time.Second

// Stop cancels the Runners, waits for them, then calls the active Stoppers
// one at a time in reverse registration order. The Runner drain and the
// Stoppers share one deadline, StopTimeout; a Stopper called after it gets
// StopGrace (see StopGrace). Stop keeps going past failures, and the result
// joins every error. Stop ignores the cancellation of ctx (shutdown usually
// follows a cancelled context) but keeps its values. The one exception is
// the DrainDelay wait: a ctx cancelled during it ends it early.
//
// A Runner or Stopper that overruns its deadline is reported and left
// running: the App stops waiting for it but cannot interrupt it.
//
// Stop is safe to call more than once and from Build's error path on an App
// that never started: then it releases what the constructors acquired.
func (a *App) Stop(ctx context.Context) error {
	a.stopOnce.Do(func() { a.stopErr = a.stop(ctx) })
	return a.stopErr
}

func (a *App) stop(ctx context.Context) error {
	a.mu.Lock()
	wasRunning := a.state == stateRunning
	a.state = stateStopping
	cancelRun := a.cancelRun
	a.mu.Unlock()
	close(a.stopping)

	logger := a.logger()
	logger.InfoContext(ctx, "nano: stopping")
	began := time.Now()
	base := context.WithoutCancel(ctx)
	deadline := began.Add(a.cfg.StopTimeout)

	var errs []error
	if cancelRun != nil {
		if wasRunning && a.cfg.DrainDelay > 0 {
			a.drain(ctx)
		}
		cancelRun()
		if err := a.drainRunners(deadline); err != nil {
			logger.ErrorContext(ctx, "nano: runners did not stop", nanolog.Err(err))
			errs = append(errs, err)
		}
	}

	a.mu.Lock()
	hooks := slices.Clone(a.hooks)
	a.mu.Unlock()
	for _, h := range slices.Backward(hooks) {
		a.mu.Lock()
		active := h.active && h.stop != nil
		h.active = false
		a.mu.Unlock()
		if !active {
			continue
		}
		hookCtx, cancel := stopContext(base, deadline)
		err := call(hookCtx, h.stop)
		cancel()
		if err != nil {
			err = fmt.Errorf("nano: stop %s: %w", h.name, err)
			logger.ErrorContext(ctx, "nano: stop failed", nanolog.Err(err), slog.String(nanolog.KeyComponent, h.name))
			errs = append(errs, err)
		}
	}

	a.mu.Lock()
	a.state = stateStopped
	a.mu.Unlock()
	// Each failure above was logged at Error where it happened; this summary
	// only counts them, so one failure gives one Error line.
	logger.InfoContext(ctx, "nano: stopped",
		slog.Int("failures", len(errs)), slog.Duration("took", time.Since(began)))
	return errors.Join(errs...)
}

// drain keeps the Runners serving for DrainDelay: readiness already answers
// unavailable, and the load balancers need that long to see it. Start
// checked that DrainDelay leaves the Runners StopGrace of StopTimeout.
//
// A ctx cancelled during the drain ends it early, so a caller can cut the
// wait short. A ctx already cancelled when the drain begins does not: a
// shutdown usually follows a cancelled context, and the drain is for it.
func (a *App) drain(ctx context.Context) {
	logger := a.logger()
	logger.InfoContext(ctx, "nano: draining", slog.Duration("drain_delay", a.cfg.DrainDelay))
	cut := ctx.Done()
	if ctx.Err() != nil {
		cut = nil
	}
	timer := time.NewTimer(a.cfg.DrainDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-cut:
		logger.InfoContext(ctx, "nano: drain cut short", slog.Any(nanolog.KeyCause, context.Cause(ctx)))
	}
}

// drainRunners waits for every Runner to return, until deadline.
func (a *App) drainRunners(deadline time.Time) error {
	drained := make(chan struct{})
	go func() { a.runWG.Wait(); close(drained) }()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-drained:
		return nil
	case <-timer.C:
	}
	select {
	case <-drained: // the last Runner returned at the deadline
		return nil
	default:
	}
	a.mu.Lock()
	names := make([]string, 0, len(a.running))
	for name := range a.running {
		names = append(names, name)
	}
	a.mu.Unlock()
	slices.Sort(names)
	return fmt.Errorf("nano: runners %v did not return before the stop deadline: %w", names, context.DeadlineExceeded)
}

// stopContext returns the context one Stopper runs under: the shared stop
// deadline, or StopGrace from now when that is later.
func stopContext(base context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if floor := time.Now().Add(StopGrace); deadline.Before(floor) {
		deadline = floor
	}
	return context.WithDeadline(base, deadline)
}

// runGuarded runs fn and turns a panic into an error.
func runGuarded(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(ctx)
}

// call runs fn and returns its error. If ctx is done before fn returns, call
// stops waiting and reports why: the deadline passed, or ctx was cancelled.
// A result fn produces at the same moment wins over ctx. A panic in fn
// becomes an error. fn keeps running in the background after call gave up:
// the App cannot interrupt it.
func call(ctx context.Context, fn func(context.Context) error) error {
	_, err := callTracked(ctx, fn)
	return err
}

// callTracked is call that also hands back, when it gave up on fn, the
// channel on which fn's result arrives later; nil when fn returned.
func callTracked(ctx context.Context, fn func(context.Context) error) (late <-chan error, err error) {
	done := make(chan error, 1)
	go func() { done <- runGuarded(ctx, fn) }()
	select {
	case err = <-done:
		return nil, err
	case <-ctx.Done():
	}
	select {
	case err = <-done:
		return nil, err
	default:
	}
	cause := context.Cause(ctx)
	if errors.Is(cause, context.DeadlineExceeded) {
		return done, fmt.Errorf("did not return in time: %w", cause)
	}
	return done, fmt.Errorf("interrupted: %w", cause)
}
