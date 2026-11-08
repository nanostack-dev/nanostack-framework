package nanofx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"
)

// params are the dependencies of Lifecycle. fx fills a generic In struct
// through reflection on its instantiated type.
type params[T any] struct {
	fx.In

	Lifecycle  fx.Lifecycle
	Shutdowner fx.Shutdowner
	Value      T
	Registry   *health.Registry `optional:"true"`
}

// Lifecycle registers the lifecycle methods of the T in the fx graph, the way
// core/nano.App.Manage does:
//
//   - Start runs in OnStart.
//   - Run starts in a goroutine after Start. OnStop cancels its context and
//     waits for it. A Run that returns before OnStop shuts the application
//     down with exit code 1, and OnStop returns its error.
//   - Stop runs in OnStop, after Run has returned. When Run does not return
//     before the OnStop deadline, Stop runs anyway, while Run may still run,
//     and it gets nano.StopGrace from its call instead of the expired
//     context, as a late Stopper does in nano.App.
//   - CheckHealth is registered as name in the *health.Registry, when the
//     graph provides one, so the legacy /health body reports it.
//
// The methods are found on the dynamic type, so a T that is an interface,
// such as nanocache.Store, gets the lifecycle of the value behind it. A nil
// value registers nothing. Lifecycle also forces the construction of the T.
func Lifecycle[T any](name string) fx.Option {
	return fx.Invoke(func(p params[T]) {
		register(name, any(p.Value), p.Lifecycle, p.Shutdowner, p.Registry)
	})
}

type (
	starter interface {
		Start(ctx context.Context) error
	}
	stopper interface {
		Stop(ctx context.Context) error
	}
	runner interface {
		Run(ctx context.Context) error
	}
	checker interface {
		CheckHealth(ctx context.Context) error
	}
)

func register(name string, v any, lc fx.Lifecycle, shutdowner fx.Shutdowner, registry *health.Registry) {
	if v == nil {
		return
	}
	if c, ok := v.(checker); ok && registry != nil {
		registry.Register(health.NewChecker(name, c.CheckHealth))
	}
	s, _ := v.(starter)
	st, _ := v.(stopper)
	r, _ := v.(runner)
	if s == nil && st == nil && r == nil {
		return
	}
	run := &running{name: name}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if s != nil {
				if err := s.Start(ctx); err != nil {
					return fmt.Errorf("nanofx: start %s: %w", name, err)
				}
			}
			if r != nil {
				run.start(r, shutdowner)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			var errs []error
			if r != nil {
				errs = append(errs, run.stop(ctx))
			}
			if st != nil {
				stopCtx, cancel := stopContext(ctx)
				err := st.Stop(stopCtx)
				cancel()
				if err != nil {
					errs = append(errs, fmt.Errorf("nanofx: stop %s: %w", name, err))
				}
			}
			return errors.Join(errs...)
		},
	})
}

// stopContext returns the context Stop runs under: the deadline of the OnStop
// context, or nano.StopGrace from now when that is later. So a Run that used
// up the deadline does not leave Stop an expired context. A context without
// a deadline is kept as it is.
func stopContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	if floor := time.Now().Add(nano.StopGrace); deadline.Before(floor) {
		deadline = floor
	}
	return context.WithDeadline(context.WithoutCancel(ctx), deadline)
}

// running is one Run goroutine.
type running struct {
	name   string
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	early  bool
}

func (r *running) start(component runner, shutdowner fx.Shutdowner) {
	// The run context outlives OnStart, whose context ends when start ends.
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go func() {
		defer close(r.done)
		r.err = component.Run(ctx)
		if ctx.Err() == nil {
			// Run returned before OnStop: the component stopped serving.
			r.early = true
			_ = shutdowner.Shutdown(fx.ExitCode(1))
		}
	}()
}

func (r *running) stop(ctx context.Context) error {
	r.cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		return fmt.Errorf("nanofx: %s did not return before the stop deadline: %w", r.name, ctx.Err())
	}
	switch {
	case r.early && r.err == nil:
		return fmt.Errorf("nanofx: %s returned before stop", r.name)
	case r.err != nil && !errors.Is(r.err, context.Canceled):
		return fmt.Errorf("nanofx: run %s: %w", r.name, r.err)
	}
	return nil
}
