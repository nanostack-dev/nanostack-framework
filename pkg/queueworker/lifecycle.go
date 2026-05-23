package queueworker

import (
	"context"

	"github.com/rs/zerolog"
)

// Runner is the common shape for durable queue workers.
type Runner interface {
	Run(context.Context) error
}

type Started struct {
	Cancel context.CancelFunc
	Done   <-chan struct{}
}

// Start runs a worker with cancellation and standard error logging.
func Start(parent context.Context, runner Runner, logger zerolog.Logger, component string) Started {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error().Err(err).Str("component", component).Msg("queue worker stopped with error")
		}
	}()
	return Started{Cancel: cancel, Done: done}
}

// Stop cancels a started worker and waits for completion or stop context expiry.
func Stop(ctx context.Context, started Started) error {
	if started.Cancel != nil {
		started.Cancel()
	}
	if started.Done == nil {
		return nil
	}
	select {
	case <-started.Done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
