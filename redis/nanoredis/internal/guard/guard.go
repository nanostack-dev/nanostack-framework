// Package guard is what the limits of nanoredis share: the Redis client of a
// store, the validation of names and keys, the failure policy, the per-call
// deadline and the throttled warning. The public names are re-exported by
// package nanoredis.
package guard

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
)

const (
	// DefaultTimeout is the deadline of one call when WithTimeout is not
	// given. A limit sits on the request path, so it answers by the policy
	// rather than hold the request while Redis is slow.
	DefaultTimeout = 100 * time.Millisecond

	// warnInterval spaces the warnings of one limiter whose Redis fails.
	warnInterval = time.Minute
)

var (
	// ErrUnavailable wraps every error of a call that Redis did not answer:
	// no Redis client, a Redis error, or the call's deadline. The call still
	// returns the answer of the policy.
	ErrUnavailable = errors.New("nanoredis: redis unavailable")
	// ErrInvalidArgument wraps an invalid configuration or call argument.
	ErrInvalidArgument = errors.New("nanoredis: invalid argument")

	errNoClient = errors.New("no redis client")
)

// Policy is the answer of a call that Redis did not answer.
type Policy int

const (
	// FailOpen admits the event or grants the permit.
	FailOpen Policy = iota
	// FailClosed refuses the event or the permit.
	FailClosed
)

func (p Policy) String() string {
	if p == FailClosed {
		return "fail-closed"
	}
	return "fail-open"
}

// Settings are the options of one limiter.
type Settings struct {
	Policy  Policy
	Timeout time.Duration
	Logger  *slog.Logger
}

// Option changes Settings.
type Option func(*Settings)

// WithPolicy sets the policy.
func WithPolicy(policy Policy) Option { return func(s *Settings) { s.Policy = policy } }

// WithTimeout sets the per-call deadline; zero or less keeps only the
// caller's context.
func WithTimeout(timeout time.Duration) Option { return func(s *Settings) { s.Timeout = timeout } }

// WithLogger sets the logger of the throttled warning.
func WithLogger(logger *slog.Logger) Option { return func(s *Settings) { s.Logger = logger } }

// ClientSource is a store that may hold a Redis client, such as
// cache.Store. A nil source or a nil client means no Redis.
type ClientSource interface {
	RedisClient() *redis.Client
}

// ClientOf returns the client of source, or nil.
func ClientOf(source ClientSource) *redis.Client {
	if source == nil {
		return nil
	}
	return source.RedisClient()
}

// Runner runs the scripts of one limiter.
type Runner struct {
	client   *redis.Client
	name     string
	settings Settings
	lastWarn atomic.Int64 // unix nanoseconds of the last warning
}

// NewRunner returns the runner of the limiter name over client, which may be
// nil.
func NewRunner(client *redis.Client, name string, opts []Option) (*Runner, error) {
	if name == "" || strings.ContainsAny(name, "*?[]") || strings.HasSuffix(name, ":") {
		return nil, fmt.Errorf("%w: name %q must be non-empty, without a glob character or a trailing ':'",
			ErrInvalidArgument, name)
	}
	settings := Settings{Policy: FailOpen, Timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&settings)
	}
	return &Runner{client: client, name: name, settings: settings}, nil
}

// Name is the limiter's name.
func (r *Runner) Name() string { return r.name }

// FailOpen reports whether the policy admits when Redis does not answer.
func (r *Runner) FailOpen() bool { return r.settings.Policy == FailOpen }

// Key returns the Redis key of a caller's key: name:key.
func (r *Runner) Key(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: empty key", ErrInvalidArgument)
	}
	return r.name + ":" + key, nil
}

// Unavailable wraps err in ErrUnavailable and logs the throttled warning.
func (r *Runner) Unavailable(ctx context.Context, err error) error {
	err = fmt.Errorf("%w: %s: %w", ErrUnavailable, r.name, err)
	if r.settings.Logger != nil && r.warnDue() {
		r.settings.Logger.WarnContext(ctx, "shared limit could not reach redis; applying its failure policy",
			slog.String("limit", r.name), slog.String("policy", r.settings.Policy.String()), log.Err(err))
	}
	return err
}

func (r *Runner) warnDue() bool {
	now := time.Now().UnixNano()
	last := r.lastWarn.Load()
	return now-last >= int64(warnInterval) && r.lastWarn.CompareAndSwap(last, now)
}

// Eval runs script on key within the deadline and checks that it returns
// want integers. Without a client it fails at once.
func (r *Runner) Eval(ctx context.Context, script *redis.Script, key string, want int, args ...any) ([]int64, error) {
	values, err := r.eval(ctx, script, key, args...)
	if err == nil && len(values) != want {
		err = fmt.Errorf("script returned %d values, want %d", len(values), want)
	}
	return values, err
}

func (r *Runner) eval(ctx context.Context, script *redis.Script, key string, args ...any) ([]int64, error) {
	if r.client == nil {
		return nil, errNoClient
	}
	if r.settings.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.settings.Timeout)
		defer cancel()
	}
	run := func() ([]int64, error) {
		values, err := script.Run(ctx, r.client, []string{key}, args...).Int64Slice()
		if ctxErr := contextErr(ctx); err != nil && ctxErr != nil && !errors.Is(err, ctxErr) {
			// A client that applies deadlines reports an i/o timeout;
			// say which deadline it was.
			err = fmt.Errorf("%w: %w", ctxErr, err)
		}
		return values, err
	}
	if r.client.Options().ContextTimeoutEnabled {
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
	case res := <-done:
		return res.values, res.err
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

// ReplicateEffects makes a server older than Redis 5 replicate the writes of
// a script rather than the script, which reads the clock. Newer servers
// always do. Every script starts with it.
const ReplicateEffects = `
if redis.replicate_commands then
	redis.replicate_commands()
end
`
