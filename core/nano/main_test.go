package nano

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRunExitCodes(t *testing.T) {
	quiet := func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
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

	// SIGTERM while Build runs a migration or pings a server is a requested
	// shutdown too, as it is one step later, during start.
	t.Run("shutdown during build", func(t *testing.T) {
		for name, cause := range map[string]func(ctx context.Context) error{
			"migration stopped": func(ctx context.Context) error {
				return fmt.Errorf("nano: invoke migrations: %w", stopped{context.Cause(ctx)})
			},
			"ping cancelled": func(context.Context) error {
				return errors.Join(fmt.Errorf("nanopg: ping db:5432: %w", context.Canceled))
			},
		} {
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				code := run(ctx, func(ctx context.Context) (*App, error) {
					cancel() // stands in for SIGTERM
					return nil, cause(ctx)
				}, quiet)
				if code != ExitOK {
					t.Fatalf("exit = %d, want %d", code, ExitOK)
				}
			})
		}
	})

	t.Run("build failure besides the shutdown", func(t *testing.T) {
		for name, err := range map[string]error{
			"stop failed too": errors.Join(fmt.Errorf("ping: %w", context.Canceled), errors.New("flush failed")),
			"dirty migration": errors.New("nanomigrate: migrate (stop requested: context canceled): dirty version 3"),
		} {
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				code := run(ctx, func(context.Context) (*App, error) { cancel(); return nil, err }, quiet)
				if code != ExitFailure {
					t.Fatalf("exit = %d, want %d", code, ExitFailure)
				}
			})
		}
	})

	t.Run("cancellation without a shutdown request", func(t *testing.T) {
		code := run(t.Context(), func(context.Context) (*App, error) {
			return nil, fmt.Errorf("nanopg: ping: %w", context.Canceled)
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

// stopped has the shape of nanomigrate's stop error: a marker over the
// cause, in one chain.
type stopped struct{ cause error }

func (e stopped) Error() string { return "stopped before the last migration ran: " + e.cause.Error() }
func (e stopped) Unwrap() error { return e.cause }
