package nano

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRunExitCodes(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	blockUntilDone := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

	t.Run("build failure", func(t *testing.T) {
		code := run(t.Context(), func(context.Context) (*App, error) { return nil, errors.New("bad config") }, quiet)
		if code != ExitFailure {
			t.Fatalf("exit = %d, want %d", code, ExitFailure)
		}
	})

	t.Run("clean shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		stopped := false
		code := run(ctx, func(context.Context) (*App, error) {
			app := newTestApp(AppConfig{})
			app.OnRun("http", blockUntilDone)
			app.OnStop("pool", func(context.Context) error { stopped = true; return nil })
			time.AfterFunc(10*time.Millisecond, cancel) // stands in for SIGTERM
			return app, nil
		}, quiet)
		if code != ExitOK || !stopped {
			t.Fatalf("exit = %d stopped = %v, want %d true", code, stopped, ExitOK)
		}
	})

	// SIGTERM while a Starter is still running is a requested shutdown.
	t.Run("shutdown during start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		stopped := false
		code := run(ctx, func(context.Context) (*App, error) {
			app := newTestApp(AppConfig{StartTimeout: time.Minute})
			app.OnStop("pool", func(context.Context) error { stopped = true; return nil })
			app.OnStart("migrate", blockUntilDone)
			time.AfterFunc(10*time.Millisecond, cancel) // stands in for SIGTERM
			return app, nil
		}, quiet)
		if code != ExitOK || !stopped {
			t.Fatalf("exit = %d stopped = %v, want %d true", code, stopped, ExitOK)
		}
	})

	t.Run("shutdown during start with a failed stop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		code := run(ctx, func(context.Context) (*App, error) {
			app := newTestApp(AppConfig{StartTimeout: time.Minute})
			app.OnStop("pool", func(context.Context) error { return errors.New("flush failed") })
			app.OnStart("migrate", blockUntilDone)
			time.AfterFunc(10*time.Millisecond, cancel)
			return app, nil
		}, quiet)
		if code != ExitFailure {
			t.Fatalf("exit = %d, want %d", code, ExitFailure)
		}
	})

	t.Run("runner failure", func(t *testing.T) {
		code := run(t.Context(), func(context.Context) (*App, error) {
			app := newTestApp(AppConfig{})
			app.OnRun("http", func(context.Context) error { return errors.New("port in use") })
			return app, nil
		}, quiet)
		if code != ExitFailure {
			t.Fatalf("exit = %d, want %d", code, ExitFailure)
		}
	})
}
