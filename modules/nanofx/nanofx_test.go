package nanofx_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/modules/nanofx"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"
)

// component records the lifecycle calls of a nano-shaped component.
type component struct {
	mu       sync.Mutex
	calls    []string
	runErr   error
	runEarly bool
	healthy  error
}

func (c *component) record(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *component) recorded() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

func (c *component) Start(context.Context) error { c.record("start"); return nil }

func (c *component) Run(ctx context.Context) error {
	c.record("run")
	if c.runEarly {
		return c.runErr
	}
	<-ctx.Done()
	c.record("run returned")
	return nil
}

func (c *component) Stop(context.Context) error { c.record("stop"); return nil }

func (c *component) CheckHealth(context.Context) error { return c.healthy }

func newApp(t *testing.T, c *component, extra ...fx.Option) *fx.App {
	t.Helper()
	app := fx.New(append([]fx.Option{
		fx.NopLogger,
		fx.Supply(c),
		nanofx.Lifecycle[*component]("component"),
	}, extra...)...)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	return app
}

func waitFor(t *testing.T, c *component, call string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(c.recorded(), call) {
		if time.Now().After(deadline) {
			t.Fatalf("%s was not called; calls: %v", call, c.recorded())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLifecycleRunsStartRunAndStopInNanoOrder(t *testing.T) {
	c := &component{}
	app := newApp(t, c)

	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "run")
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	want := []string{"start", "run", "run returned", "stop"}
	if got := c.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: Run starts after Start, and Stop runs after Run returned", got, want)
	}
}

func TestLifecycleShutsDownTheAppWhenRunReturnsEarly(t *testing.T) {
	c := &component{runEarly: true, runErr: errors.New("listen: address in use")}
	app := newApp(t, c)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	select {
	case signal := <-app.Wait():
		if signal.ExitCode != 1 {
			t.Fatalf("exit code = %d, want 1", signal.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an early Run did not shut the application down")
	}
	err := app.Stop(t.Context())
	if err == nil || !errors.Is(err, c.runErr) {
		t.Fatalf("Stop = %v, want the Run error", err)
	}
}

func TestLifecycleRegistersCheckHealthInTheRegistry(t *testing.T) {
	c := &component{healthy: errors.New("ping failed")}
	registry := health.NewRegistry()
	app := newApp(t, c, fx.Supply(registry))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	results, healthy := registry.Evaluate(t.Context())

	if healthy || results["component"] != "UNHEALTHY: ping failed" {
		t.Fatalf("registry = (%v, %v), want component unhealthy", results, healthy)
	}
}

// valueOnly has no lifecycle method.
type valueOnly struct{}

func TestLifecycleOfAValueWithoutMethodsRegistersNothing(t *testing.T) {
	app := fx.New(fx.NopLogger, fx.Supply(valueOnly{}), nanofx.Lifecycle[valueOnly]("plain"))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// starter is an interface, as nanocache.Store is.
type starter interface{ Start(context.Context) error }

func TestLifecycleUsesTheDynamicTypeOfAnInterface(t *testing.T) {
	c := &component{}
	app := fx.New(
		fx.NopLogger,
		fx.Provide(func() starter { return c }),
		nanofx.Lifecycle[starter]("component"),
	)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "run")
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := c.recorded(); !slices.Contains(got, "stop") {
		t.Fatalf("calls = %v, want Stop through the dynamic type", got)
	}
}

// stubborn is a Runner and Stopper whose Run ignores its cancellation until
// release is closed.
type stubborn struct {
	release  chan struct{}
	stopped  chan struct{}
	stopErr  error         // ctx.Err() when Stop was called
	stopLeft time.Duration // time to the deadline of Stop's context
}

func (s *stubborn) Run(context.Context) error { <-s.release; return nil }

func (s *stubborn) Stop(ctx context.Context) error {
	s.stopErr = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		s.stopLeft = time.Until(deadline)
	}
	close(s.stopped)
	return nil
}

// TestLifecycleGivesALateStopTheStopGrace checks that a Run that misses the
// stop deadline does not leave Stop an expired context: Stop gets
// nano.StopGrace, as a late Stopper does in nano.App.
func TestLifecycleGivesALateStopTheStopGrace(t *testing.T) {
	s := &stubborn{release: make(chan struct{}), stopped: make(chan struct{})}
	t.Cleanup(func() { close(s.release) })
	app := fx.New(fx.NopLogger, fx.Supply(s), nanofx.Lifecycle[*stubborn]("stubborn"))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := app.Stop(ctx)

	if err == nil {
		t.Fatal("Stop = nil, want the error of the Run that missed the deadline")
	}
	// fx returns from app.Stop at the deadline and leaves the hook running.
	select {
	case <-s.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop was not called")
	}
	if s.stopErr != nil || s.stopLeft < nano.StopGrace/2 {
		t.Fatalf("Stop got ctx.Err() = %v with %s left, want a live context with about %s",
			s.stopErr, s.stopLeft, nano.StopGrace)
	}
}
