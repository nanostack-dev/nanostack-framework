package nanoredis

import (
	"log/slog"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/internal/guard"
)

// DefaultTimeout is the deadline of one call of a limit when WithTimeout is
// not given: 100ms.
const DefaultTimeout = guard.DefaultTimeout

// FailurePolicy is the answer of a limit whose Redis did not answer.
type FailurePolicy = guard.Policy

const (
	// FailOpen admits the event or grants the permit. It suits a limit that
	// protects capacity, where an outage of Redis must not become an outage
	// of the service.
	FailOpen = guard.FailOpen
	// FailClosed refuses the event or the permit. It suits a limit that
	// protects money or a third party, where an unchecked event costs more
	// than a refused one.
	FailClosed = guard.FailClosed
)

var (
	// ErrUnavailable wraps every error of a call that Redis did not answer:
	// no Redis client, a Redis error, or the call's deadline. The call still
	// returns the answer of the FailurePolicy.
	ErrUnavailable = guard.ErrUnavailable
	// ErrInvalidArgument wraps an invalid configuration or call argument.
	ErrInvalidArgument = guard.ErrInvalidArgument
)

// Option configures a throttle or a semaphore.
type Option = guard.Option

// WithFailurePolicy sets the answer of a call that Redis did not answer.
// The default is FailOpen.
func WithFailurePolicy(policy FailurePolicy) Option { return guard.WithPolicy(policy) }

// WithTimeout bounds every call, including the wait for Redis, whatever the
// client's own timeouts. Zero or less keeps only the caller's context. The
// default is DefaultTimeout.
func WithTimeout(timeout time.Duration) Option { return guard.WithTimeout(timeout) }

// WithLogger logs a warning, at most once a minute per limiter, when a call
// falls back to the FailurePolicy. Without it nothing is logged; the error
// that the call returns still says what happened.
func WithLogger(logger *slog.Logger) Option { return guard.WithLogger(logger) }
