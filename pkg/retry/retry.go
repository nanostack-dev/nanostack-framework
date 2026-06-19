// Package retry runs an operation a bounded number of times with a fixed delay
// between attempts, stopping early on success, on a non-retryable error, or on
// context cancellation. It is intentionally small — a generic replacement for
// hand-rolled retry loops (think a minimal Spring @Retryable / resilience4j).
package retry

import (
	"context"
	"time"
)

const (
	// DefaultMaxAttempts is the default total number of attempts.
	DefaultMaxAttempts = 3
	// DefaultDelay is the default wait between attempts.
	DefaultDelay = time.Second
)

// Options configures Do. The zero value is usable but caller-supplied values are
// normalized: MaxAttempts < 1 becomes 1, a negative Delay becomes 0, and a nil
// RetryIf retries on every non-nil error.
type Options struct {
	// MaxAttempts is the total number of attempts (not retries-after-first).
	MaxAttempts int
	// Delay is the fixed wait between attempts (no backoff).
	Delay time.Duration
	// RetryIf decides whether a given error is worth retrying. When nil, every
	// non-nil error is retried.
	RetryIf func(error) bool
}

// Default returns Options with DefaultMaxAttempts attempts and DefaultDelay.
func Default() Options {
	return Options{MaxAttempts: DefaultMaxAttempts, Delay: DefaultDelay}
}

func (o Options) normalized() Options {
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 1
	}
	if o.Delay < 0 {
		o.Delay = 0
	}
	if o.RetryIf == nil {
		o.RetryIf = func(error) bool { return true }
	}
	return o
}

// Do calls fn up to opts.MaxAttempts times, waiting opts.Delay between attempts.
// It returns nil on the first success. It stops early and returns the error when
// RetryIf reports the error is not retryable. If the context is cancelled while
// waiting between attempts, Do returns the context error. After the final attempt
// the last error from fn is returned.
func Do(ctx context.Context, opts Options, fn func(ctx context.Context) error) error {
	opts = opts.normalized()

	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if !opts.RetryIf(lastErr) {
			return lastErr
		}
		if attempt == opts.MaxAttempts {
			break
		}

		if err := sleep(ctx, opts.Delay); err != nil {
			return err
		}
	}
	return lastErr
}

// sleep waits for d or until ctx is done, returning the context error if cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
