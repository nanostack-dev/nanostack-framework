package nano

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	nanolog "github.com/nanostack-dev/nanostack-framework/core/log"
)

// Exit codes returned by Main.
const (
	ExitOK      = 0 // clean shutdown after SIGINT or SIGTERM, even during build or start
	ExitFailure = 1 // build, start, Runner or stop failure
)

// Main is a service's whole entry point: it cancels a context on SIGINT or
// SIGTERM, calls build, runs the App until that signal or a Runner failure,
// stops it, and exits with ExitOK or ExitFailure. A signal during build or
// start is a clean shutdown (ExitOK) when the only error is the
// cancellation. A second signal during shutdown kills the process
// immediately.
//
// build binds the configuration and calls the generated Build; ExampleMain
// shows it.
//
// Main logs through slog.Default() as it is when it logs, not as it was when
// Main began: build usually installs the service's logger with
// slog.SetDefault, and a logger taken before that writes through the log
// package, which SetDefault routes into the new handler at Info.
func Main(build func(context.Context) (*App, error)) {
	os.Exit(run(context.Background(), build, slog.Default))
}

// run is Main without the exit. logger returns the logger for each line.
func run(parent context.Context, build func(context.Context) (*App, error), logger func() *slog.Logger) int {
	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		// Restore default signal handling once shutdown begins, so a second
		// signal terminates a shutdown that hangs.
		<-ctx.Done()
		stopSignals()
	}()

	app, err := build(ctx)
	if err != nil {
		// A signal during build, such as during a migration or the ping of a
		// server, is a requested shutdown, as it is during start. The
		// generated Build has already stopped what it built; a failure of
		// that stop is not a cancellation and keeps the exit code at 1.
		if ctx.Err() != nil && onlyCancellation(err) {
			logger().InfoContext(ctx, "nano: shutdown requested while building", nanolog.Err(err))
			return ExitOK
		}
		logger().ErrorContext(ctx, "nano: build failed", nanolog.Err(err))
		return ExitFailure
	}
	// The App logged each start, Runner and stop failure at Error where it
	// happened; logging the joined error again would count each one twice.
	if err = app.Run(ctx); err != nil && !onlyShutdownDuringStart(err) {
		logger().InfoContext(ctx, "nano: exiting", slog.Int("exit_code", ExitFailure))
		return ExitFailure
	}
	return ExitOK
}

// onlyCancellation reports whether every error joined in err ends in
// context.Canceled, which the cause of a signal context also matches: each
// branch of a join must, and a wrapping chain must end in one. A component
// that annotates the cancellation (nanomigrate's ErrStopped) keeps it in one
// chain, so a sibling failure is never mistaken for the shutdown.
func onlyCancellation(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs := joined.Unwrap()
		for _, e := range errs {
			if !onlyCancellation(e) {
				return false
			}
		}
		return len(errs) > 0
	}
	if wrapper, ok := err.(interface{ Unwrap() error }); ok {
		if inner := wrapper.Unwrap(); inner != nil {
			return onlyCancellation(inner)
		}
	}
	return errors.Is(err, context.Canceled)
}

// onlyShutdownDuringStart reports whether every error joined in err is
// ErrShutdownDuringStart: a shutdown requested while starting, after which
// every stop succeeded.
func onlyShutdownDuringStart(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if !onlyShutdownDuringStart(e) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, ErrShutdownDuringStart)
}
