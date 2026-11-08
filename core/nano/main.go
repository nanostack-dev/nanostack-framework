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
	ExitOK      = 0 // clean shutdown after SIGINT or SIGTERM, even during start
	ExitFailure = 1 // build, start, Runner or stop failure
)

// Main is a service's whole entry point: it cancels a context on SIGINT or
// SIGTERM, calls build, runs the App until that signal or a Runner failure,
// stops it, and exits with ExitOK or ExitFailure. A second signal during
// shutdown kills the process immediately.
//
//	func main() {
//		nano.Main(func(ctx context.Context) (*nano.App, error) {
//			cfg, err := config.Load()
//			if err != nil {
//				return nil, err
//			}
//			return wire.Build(ctx, cfg, wire.Overrides{})
//		})
//	}
func Main(build func(context.Context) (*App, error)) {
	os.Exit(run(context.Background(), build, slog.Default()))
}

func run(parent context.Context, build func(context.Context) (*App, error), logger *slog.Logger) int {
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
		logger.ErrorContext(ctx, "nano: build failed", nanolog.Err(err))
		return ExitFailure
	}
	// The App logged each start, Runner and stop failure at Error where it
	// happened; logging the joined error again would count each one twice.
	if err = app.Run(ctx); err != nil && !onlyShutdownDuringStart(err) {
		logger.InfoContext(ctx, "nano: exiting", slog.Int("exit_code", ExitFailure))
		return ExitFailure
	}
	return ExitOK
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
