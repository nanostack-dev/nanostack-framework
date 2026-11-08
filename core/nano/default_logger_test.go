package nano

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
)

// installDefault makes logger the slog default until the test ends, as a
// service's logging provider does during build. The tests that call it change
// process-wide state, so they never run in parallel.
func installDefault(t *testing.T, logger *slog.Logger) {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(logger)
}

// assertNoDemotedLines fails when a line arrived through the log package at
// Info with its level folded into the message, which is how a logger taken
// before slog.SetDefault writes after it.
func assertNoDemotedLines(t *testing.T, rec *logtest.Recorder) {
	t.Helper()
	for _, e := range rec.Entries() {
		if strings.Contains(e.Message, "nano: ") && !strings.HasPrefix(e.Message, "nano: ") {
			t.Errorf("line went through the log package: %s", e.Format())
		}
	}
}

// TestMainLogsThroughTheDefaultInstalledDuringBuild runs Main's loop with a
// build that installs the service's logger and then fails: the failure
// arrives at Error in that logger, not at Info through the log package.
func TestMainLogsThroughTheDefaultInstalledDuringBuild(t *testing.T) {
	logger, rec := logtest.New()
	code := run(t.Context(), func(context.Context) (*App, error) {
		installDefault(t, logger)
		return nil, errors.New("bad config")
	}, slog.Default)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	rec.AssertLogged(t, slog.LevelError, "nano: build failed")
	assertNoDemotedLines(t, rec)
}

// TestMainLogsAShutdownDuringBuildThroughTheNewDefault covers Main's other
// build line: a signal during build after the logger is installed.
func TestMainLogsAShutdownDuringBuildThroughTheNewDefault(t *testing.T) {
	logger, rec := logtest.New()
	ctx, cancel := context.WithCancel(t.Context())
	code := run(ctx, func(ctx context.Context) (*App, error) {
		installDefault(t, logger)
		cancel() // stands in for SIGTERM
		return nil, context.Cause(ctx)
	}, slog.Default)
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	rec.AssertLogged(t, slog.LevelInfo, "nano: shutdown requested while building")
	assertNoDemotedLines(t, rec)
}

// TestAppLogsThroughTheDefaultInstalledAfterNewApp follows the generated
// Build's order: NewApp first, then a provider installs the service's logger.
// The App's start, stop and exit lines reach that logger at their own level.
func TestAppLogsThroughTheDefaultInstalledAfterNewApp(t *testing.T) {
	logger, rec := logtest.New()
	code := run(t.Context(), func(context.Context) (*App, error) {
		app := NewApp("test", AppConfig{})
		installDefault(t, logger)
		app.Hook("db", nil, func(context.Context) error { return errors.New("close failed") })
		app.OnStart("cache", func(context.Context) error { return errors.New("dial refused") })
		return app, nil
	}, slog.Default)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	rec.AssertLogged(t, slog.LevelError, "nano: start failed")
	rec.AssertLogged(t, slog.LevelError, "nano: stop failed")
	rec.AssertLogged(t, slog.LevelInfo, "nano: exiting", slog.Int("exit_code", ExitFailure))
	assertNoDemotedLines(t, rec)
}

// TestSetLoggerOverridesTheDefault checks that an explicit logger still wins
// over a default installed later.
func TestSetLoggerOverridesTheDefault(t *testing.T) {
	explicit, rec := logtest.New()
	other, otherRec := logtest.New()
	app := NewApp("test", AppConfig{})
	app.SetLogger(explicit)
	installDefault(t, other)
	app.OnStart("cache", func(context.Context) error { return errors.New("dial refused") })
	if err := app.Start(t.Context()); err == nil {
		t.Fatal("Start = nil, want the start failure")
	}
	rec.AssertLogged(t, slog.LevelError, "nano: start failed")
	otherRec.AssertNotLogged(t, "nano: start failed")
}
