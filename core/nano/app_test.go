package nano

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// recorder records lifecycle events in the order they happen.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *recorder) fn(event string, err error) func(context.Context) error {
	return func(context.Context) error { r.add(event); return err }
}

func newTestApp(cfg AppConfig) *App {
	app := NewApp("test", cfg)
	app.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return app
}

func assertEvents(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("events:\n got  %q\n want %q", got, want)
	}
}

func TestStartsInOrderAndStopsInReverse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{})
		app.Hook("pool", nil, rec.fn("stop pool", nil))
		app.Hook("cache", rec.fn("start cache", nil), rec.fn("stop cache", nil))
		app.OnStart("migrate", rec.fn("start migrate", nil))
		app.OnRun("http", func(ctx context.Context) error {
			rec.add("run http")
			<-ctx.Done()
			rec.add("http done")
			return ctx.Err()
		})

		ctx, cancel := context.WithCancel(t.Context())
		errc := make(chan error, 1)
		go func() { errc <- app.Run(ctx) }()
		synctest.Wait()
		assertEvents(t, rec.get(), []string{"start cache", "start migrate", "run http"})

		cancel()
		if err := <-errc; err != nil {
			t.Fatalf("Run after cancel = %v, want nil", err)
		}
		assertEvents(t, rec.get(), []string{
			"start cache", "start migrate", "run http",
			"http done", "stop cache", "stop pool",
		})
	})
}

func TestStartFailureStopsActiveComponentsInReverse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		boom := errors.New("boom")
		app := newTestApp(AppConfig{})
		app.Hook("a", rec.fn("start a", nil), rec.fn("stop a", nil))
		app.Hook("pool", nil, rec.fn("stop pool", nil))
		app.Hook("b", rec.fn("start b", boom), rec.fn("stop b", nil))
		app.Hook("c", rec.fn("start c", nil), rec.fn("stop c", nil))
		app.Hook("conn", nil, rec.fn("stop conn", nil)) // constructed after b: still holds a resource
		app.OnRun("never", rec.fn("run never", nil))

		err := app.Start(t.Context())
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "nano: start b") {
			t.Fatalf("Start = %v, want wrapped boom naming b", err)
		}
		synctest.Wait()
		assertEvents(t, rec.get(), []string{"start a", "start b", "stop conn", "stop pool", "stop a"})

		if again := app.Start(t.Context()); again == nil {
			t.Fatal("second Start succeeded on a stopped app")
		}
	})
}

func TestStartTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{StartTimeout: 3 * time.Second})
		app.Hook("pool", nil, rec.fn("stop pool", nil))
		app.OnStart("slow", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})

		began := time.Now()
		err := app.Start(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Start = %v, want deadline exceeded", err)
		}
		if took := time.Since(began); took != 3*time.Second {
			t.Fatalf("Start took %v, want the 3s start timeout", took)
		}
		assertEvents(t, rec.get(), []string{"stop pool"})
	})
}

func TestStopIgnoresCancelledContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{})
		var gotErr error
		app.OnStop("db", func(ctx context.Context) error { gotErr = ctx.Err(); return nil })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := app.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if gotErr != nil {
			t.Fatalf("stop hook saw ctx.Err() = %v, want a live context", gotErr)
		}
	})
}

func TestRunnerErrorShutsDownTheApp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		boom := errors.New("listener died")
		app := newTestApp(AppConfig{})
		app.OnStop("pool", rec.fn("stop pool", nil))
		app.OnRun("http", func(context.Context) error {
			time.Sleep(time.Second)
			return boom
		})
		app.OnRun("worker", func(ctx context.Context) error {
			<-ctx.Done()
			rec.add("worker cancelled")
			return ctx.Err()
		})

		err := app.Run(t.Context())
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "nano: runner http") {
			t.Fatalf("Run = %v, want the http runner error", err)
		}
		if strings.Contains(err.Error(), "worker") {
			t.Fatalf("Run = %v; the cancelled worker must not be reported", err)
		}
		assertEvents(t, rec.get(), []string{"worker cancelled", "stop pool"})
	})
}

func TestRunnerReturningEarlyIsAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{})
		app.OnRun("job", func(context.Context) error { return nil })
		err := app.Run(t.Context())
		if err == nil || !strings.Contains(err.Error(), "runner job returned before shutdown") {
			t.Fatalf("Run = %v, want early-return error", err)
		}
	})
}

func TestPanicInHookBecomesError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{})
		app.OnStart("bad", func(context.Context) error { panic("kaboom") })
		err := app.Start(t.Context())
		if err == nil || !strings.Contains(err.Error(), "panic: kaboom") {
			t.Fatalf("Start = %v, want panic error", err)
		}
	})
}

func TestCancelDuringStartIsAShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{StartTimeout: time.Minute})
		app.Hook("cache", rec.fn("start cache", nil), rec.fn("stop cache", nil))
		app.OnStart("slow", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		app.OnStart("never", rec.fn("start never", nil))
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(time.Second, cancel) // SIGTERM while slow is starting
		err := app.Start(ctx)
		assertErr(t, err, "nano: start test: at slow: nano: shutdown requested while starting")
		if !errors.Is(err, ErrShutdownDuringStart) || !onlyShutdownDuringStart(err) {
			t.Fatalf("Start = %v, want only ErrShutdownDuringStart", err)
		}
		assertEvents(t, rec.get(), []string{"start cache", "stop cache"})
	})
}

func TestCancelledBeforeStartStartsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		boom := errors.New("boom")
		app := newTestApp(AppConfig{})
		app.Hook("pool", nil, rec.fn("stop pool", boom))
		app.OnStart("cache", rec.fn("start cache", nil))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := app.Start(ctx)
		if !errors.Is(err, ErrShutdownDuringStart) || !errors.Is(err, boom) {
			t.Fatalf("Start = %v, want the shutdown joined with the stop failure", err)
		}
		if onlyShutdownDuringStart(err) {
			t.Fatal("a failed stop must not count as a clean shutdown")
		}
		assertEvents(t, rec.get(), []string{"stop pool"})
	})
}

func TestRegisterAfterStartPanics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{})
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if recover() == nil {
				t.Fatal("OnStop after Start did not panic")
			}
			_ = app.Stop(t.Context())
		}()
		app.OnStop("late", func(context.Context) error { return nil })
	})
}

func TestStopEndsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := newTestApp(AppConfig{})
		app.OnRun("http", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
		errc := make(chan error, 1)
		go func() { errc <- app.Run(t.Context()) }()
		synctest.Wait()
		if err := app.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("Run after an external Stop = %v, want nil", err)
		}
	})
}
