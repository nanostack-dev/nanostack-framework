package nanolimit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

const (
	// DefaultTimeout is the deadline of one call when WithTimeout is not
	// given. A limit sits on the request path, so it answers by the policy
	// rather than hold the request while Redis is slow.
	DefaultTimeout = 100 * time.Millisecond

	// warnInterval spaces the warnings of a Limits whose Redis fails.
	warnInterval = time.Minute
)

// Module provides *Limits over the Store of redis/nanocache.
var Module = nano.Module("limit", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewLimits),
)

var (
	// ErrUnavailable wraps every error of a call that Redis did not answer:
	// no Redis client, a Redis error, or the call's deadline. The call still
	// returns the answer of the FailurePolicy.
	ErrUnavailable = errors.New("nanolimit: redis unavailable")
	// ErrInvalidArgument wraps an invalid configuration or call argument.
	ErrInvalidArgument = errors.New("nanolimit: invalid argument")
	// ErrPermitNotHeld is returned by Semaphore.Release and Semaphore.Extend
	// for a permit that is not held any more: released already, or its lease
	// ended and the permit was reclaimed.
	ErrPermitNotHeld = errors.New("nanolimit: permit not held")

	errNoClient = errors.New("no redis client")
)

// FailurePolicy is the answer of a call that Redis did not answer.
type FailurePolicy int

const (
	// FailOpen admits the event or grants the permit. It suits a limit that
	// protects capacity, where an outage of Redis must not become an outage
	// of the service.
	FailOpen FailurePolicy = iota
	// FailClosed refuses the event or the permit. It suits a limit that
	// protects money or a third party, where an unchecked event costs more
	// than a refused one.
	FailClosed
)

func (p FailurePolicy) String() string {
	if p == FailClosed {
		return "fail-closed"
	}
	return "fail-open"
}

// Option configures Limits and the limiters built from it. An option given
// to RateLimiter or Semaphore applies on top of those of the Limits.
type Option func(*options)

type options struct {
	policy  FailurePolicy
	timeout time.Duration
	logger  *slog.Logger
}

// WithFailurePolicy sets the answer of a call that Redis did not answer.
// The default is FailOpen.
func WithFailurePolicy(policy FailurePolicy) Option {
	return func(o *options) { o.policy = policy }
}

// WithTimeout bounds every call, including the wait for Redis, whatever the
// client's own timeouts. Zero or less keeps only the caller's context. The
// default is DefaultTimeout.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) { o.timeout = timeout }
}

// WithLogger logs a warning, at most once a minute per Limits, when a call
// falls back to the FailurePolicy. Without it nothing is logged; the error
// that the call returns still says what happened.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// Limits builds the rate limiters and semaphores of one Redis client.
type Limits struct {
	client *redis.Client
	opts   options
	warner *warner
}

// New returns the Limits over client. A nil client is valid: every call then
// returns the answer of the FailurePolicy with an error that wraps
// ErrUnavailable.
func New(client *redis.Client, opts ...Option) *Limits {
	o := options{policy: FailOpen, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	return &Limits{client: client, opts: o, warner: &warner{}}
}

// FromStore returns the Limits over the Redis client of store. The no-op
// store has no client: see New.
func FromStore(store nanocache.Store, opts ...Option) *Limits {
	var client *redis.Client
	if store != nil {
		client = store.RedisClient()
	}
	return New(client, opts...)
}

// NewLimits is the provider of Module: FromStore with FailOpen, the default
// timeout and a logger. It warns once when the store has no Redis.
func NewLimits(ctx context.Context, store nanocache.Store, logger *slog.Logger) *Limits {
	logger = logger.With(slog.String(log.KeyComponent, "limit"))
	limits := FromStore(store, WithLogger(logger))
	if limits.client == nil {
		logger.WarnContext(ctx, "no redis for shared limits; every limit applies its failure policy",
			slog.String("policy", limits.opts.policy.String()))
	}
	return limits
}

// RateLimiter returns the limiter of rate for the keys under name. Two
// limiters with the same name share their counts, so give each limit its own
// name, and change the name when a key must start over.
func (l *Limits) RateLimiter(name string, rate Rate, opts ...Option) (RateLimiter, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	if err := rate.validate(); err != nil {
		return nil, err
	}
	return &rateLimiter{base: l.base(name, opts), rate: rate}, nil
}

// Semaphore returns the semaphore of permits for each key under name. The
// count is checked on every call, so a new permits applies at once to the
// permits leased after it.
func (l *Limits) Semaphore(name string, permits int, opts ...Option) (Semaphore, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	if permits < 1 {
		return nil, fmt.Errorf("%w: permits %d, want at least 1", ErrInvalidArgument, permits)
	}
	return &semaphore{base: l.base(name, opts), permits: permits}, nil
}

func (l *Limits) base(name string, opts []Option) base {
	o := l.opts
	for _, opt := range opts {
		opt(&o)
	}
	return base{client: l.client, name: name, opts: o, warner: l.warner}
}

func validName(name string) error {
	if name == "" || strings.ContainsAny(name, "*?[]") || strings.HasSuffix(name, ":") {
		return fmt.Errorf("%w: name %q must be non-empty, without a glob character or a trailing ':'",
			ErrInvalidArgument, name)
	}
	return nil
}

// base is what a rate limiter and a semaphore share: the client, the Redis
// key of a caller's key, the deadline and the failure path.
type base struct {
	client *redis.Client
	name   string
	opts   options
	warner *warner
}

func (b base) redisKey(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: empty key", ErrInvalidArgument)
	}
	return b.name + ":" + key, nil
}

// unavailable wraps err in ErrUnavailable and logs the throttled warning.
func (b base) unavailable(ctx context.Context, err error) error {
	err = fmt.Errorf("%w: %s: %w", ErrUnavailable, b.name, err)
	if b.opts.logger != nil && b.warner.due() {
		b.opts.logger.WarnContext(ctx, "shared limit could not reach redis; applying its failure policy",
			slog.String("limit", b.name), slog.String("policy", b.opts.policy.String()), log.Err(err))
	}
	return err
}

// eval runs script against key within the deadline. Without a client it
// fails at once.
func (b base) eval(ctx context.Context, script *redis.Script, key string, args ...any) ([]int64, error) {
	if b.client == nil {
		return nil, errNoClient
	}
	if b.opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.opts.timeout)
		defer cancel()
	}
	run := func() ([]int64, error) {
		values, err := script.Run(ctx, b.client, []string{key}, args...).Int64Slice()
		if ctxErr := contextErr(ctx); err != nil && ctxErr != nil && !errors.Is(err, ctxErr) {
			// A client that applies deadlines reports an i/o timeout;
			// say which deadline it was.
			err = fmt.Errorf("%w: %w", ctxErr, err)
		}
		return values, err
	}
	if b.client.Options().ContextTimeoutEnabled {
		return run()
	}
	// A client that ignores context deadlines would hold the call until
	// its read timeout, so the call waits for the context instead and
	// leaves the command to finish on its own.
	type result struct {
		values []int64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		values, err := run()
		done <- result{values, err}
	}()
	select {
	case r := <-done:
		return r.values, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// contextErr is ctx.Err, or DeadlineExceeded once the deadline has passed:
// a connection deadline can fire just before the context's own timer.
func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// warner lets one warning through per warnInterval.
type warner struct {
	last atomic.Int64 // unix nanoseconds of the last warning
}

func (w *warner) due() bool {
	now := time.Now().UnixNano()
	last := w.last.Load()
	return now-last >= int64(warnInterval) && w.last.CompareAndSwap(last, now)
}
