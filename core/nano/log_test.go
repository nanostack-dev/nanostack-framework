package nano

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	nanolog "github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
)

// TestLifecycleErrorsUseCoreLogKeys checks that lifecycle failures are logged
// at Error under core/log's keys (error, component), like every other line.
func TestLifecycleErrorsUseCoreLogKeys(t *testing.T) {
	logger, rec := logtest.New()
	boom := errors.New("boom")
	stopErr := errors.New("flush failed")
	app := NewApp("test", AppConfig{})
	app.SetLogger(logger)
	app.Hook("db", func(context.Context) error { return nil }, func(context.Context) error { return stopErr })
	app.Hook("cache", func(context.Context) error { return boom }, nil)

	if err := app.Start(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("Start = %v, want boom", err)
	}
	for _, msg := range []string{"nano: start failed", "nano: stop failed"} {
		entry := rec.AssertLogged(t, slog.LevelError, msg)
		if _, ok := entry.Value(nanolog.KeyError); !ok {
			t.Errorf("%s: no %q attribute: %s", msg, nanolog.KeyError, entry.Format())
		}
		if _, ok := entry.Value("err"); ok {
			t.Errorf("%s: logged under \"err\": %s", msg, entry.Format())
		}
	}
	rec.AssertLogged(t, slog.LevelError, "nano: start failed", slog.String(nanolog.KeyComponent, "cache"))
	rec.AssertLogged(t, slog.LevelError, "nano: stop failed", slog.String(nanolog.KeyComponent, "db"))
	rec.AssertLogged(t, slog.LevelInfo, "nano: stopped", slog.Int("failures", 1))
}

// TestEachFailureIsLoggedOnce runs Main's loop with one failing Starter and
// one failing Stopper: two failures give two Error lines, and the summaries
// add none.
func TestEachFailureIsLoggedOnce(t *testing.T) {
	logger, rec := logtest.New()
	code := run(t.Context(), func(context.Context) (*App, error) {
		app := NewApp("test", AppConfig{})
		app.SetLogger(logger)
		app.Hook("db", func(context.Context) error { return nil },
			func(context.Context) error { return errors.New("close failed") })
		app.OnStart("cache", func(context.Context) error { return errors.New("dial refused") })
		return app, nil
	}, func() *slog.Logger { return logger })
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	var errorLines []string
	for _, e := range rec.Entries() {
		if e.Level >= slog.LevelError {
			errorLines = append(errorLines, e.Format())
		}
	}
	if len(errorLines) != 2 {
		t.Fatalf("got %d Error lines, want 2 (one per failure):\n%s", len(errorLines), strings.Join(errorLines, "\n"))
	}
	rec.AssertLogged(t, slog.LevelInfo, "nano: exiting", slog.Int("exit_code", ExitFailure))
}

// TestStarterFailureDuringShutdownIsAFailure covers a Starter that fails for
// its own reason while a shutdown is requested: its error is logged at Error
// and returned, so Main exits 1.
func TestStarterFailureDuringShutdownIsAFailure(t *testing.T) {
	logger, rec := logtest.New()
	migrationErr := errors.New("migration 42 failed: disk full")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app := NewApp("test", AppConfig{})
	app.SetLogger(logger)
	app.OnStart("migrate", func(context.Context) error {
		cancel()
		return migrationErr
	})
	err := app.Run(ctx)
	if !errors.Is(err, migrationErr) || !errors.Is(err, ErrShutdownDuringStart) {
		t.Fatalf("Run = %v, want the migration error joined with ErrShutdownDuringStart", err)
	}
	if onlyShutdownDuringStart(err) {
		t.Fatalf("Run = %v counts as a clean shutdown; Main would exit 0", err)
	}
	entry := rec.AssertLogged(t, slog.LevelError, "nano: start failed", slog.String(nanolog.KeyComponent, "migrate"))
	if got := entry.String(nanolog.KeyError); !strings.Contains(got, "disk full") {
		t.Errorf("start failed error = %q, want the migration error", got)
	}
	rec.AssertNotLogged(t, "nano: shutdown requested while starting")
}

// TestInvalidDrainDelayIsLogged runs Main's loop with a DrainDelay that
// leaves no StopGrace: Start fails before any Starter, and the reason is
// logged at Error, or the process would exit 1 with no reason.
func TestInvalidDrainDelayIsLogged(t *testing.T) {
	logger, rec := logtest.New()
	code := run(t.Context(), func(context.Context) (*App, error) {
		app := NewApp("test", AppConfig{DrainDelay: 15 * time.Second, StopTimeout: 15 * time.Second})
		app.SetLogger(logger)
		return app, nil
	}, func() *slog.Logger { return logger })
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	entry := rec.AssertLogged(t, slog.LevelError, "nano: start failed")
	if got := entry.String(nanolog.KeyError); !strings.Contains(got, "drain_delay") {
		t.Errorf("start failed error = %q, want the drain_delay error", got)
	}
}
