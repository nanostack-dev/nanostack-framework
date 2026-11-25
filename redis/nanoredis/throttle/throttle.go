// Package throttle is a per-key rate limiter shared by every replica of a
// service through Redis.
//
// A Limiter admits Rate.Limit events per Rate.Period for each key, of which
// up to Rate.Burst may come at once:
//
//	calls, err := throttle.New(store, "mcp:calls", throttle.PerMinute(120))
//	decision, err := calls.Allow(ctx, userID)
//	if !decision.Allowed {
//		// 429, Retry-After: decision.RetryAfter
//	}
//
// Each call is one atomic script on one key that reads the clock of the
// Redis server. Without Redis, a call returns the answer of the
// nanoredis.FailurePolicy with an error that wraps nanoredis.ErrUnavailable.
package throttle

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/internal/guard"
)

// Rate is Limit events per Period, of which up to Burst may come at once.
//
// The limiter spaces events Period/Limit apart on average and lets a key
// that has been idle send Burst events back to back. A key that keeps
// sending gets one event every Period/Limit, never a full new Burst at the
// start of each Period as a fixed window would allow.
type Rate struct {
	// Limit is the events per Period. It must be at least 1.
	Limit int
	// Period is the time over which Limit events are admitted.
	Period time.Duration
	// Burst is the most events admitted at once. Zero means Limit.
	Burst int
}

// PerSecond is n events per second, all of which may come at once.
func PerSecond(n int) Rate { return Rate{Limit: n, Period: time.Second} }

// PerMinute is n events per minute, all of which may come at once.
func PerMinute(n int) Rate { return Rate{Limit: n, Period: time.Minute} }

// PerHour is n events per hour, all of which may come at once.
func PerHour(n int) Rate { return Rate{Limit: n, Period: time.Hour} }

// WithBurst returns the rate with burst as its Burst.
func (r Rate) WithBurst(burst int) Rate {
	r.Burst = burst
	return r
}

func (r Rate) burst() int {
	if r.Burst == 0 {
		return r.Limit
	}
	return r.Burst
}

// spacing is the average time between two events, in microseconds: the
// unit of the script, which reads the clock of the Redis server.
func (r Rate) spacing() int64 { return r.Period.Microseconds() / int64(r.Limit) }

func (r Rate) validate() error {
	switch {
	case r.Limit < 1:
		return fmt.Errorf("%w: rate limit %d, want at least 1", nanoredis.ErrInvalidArgument, r.Limit)
	case r.Period <= 0:
		return fmt.Errorf("%w: rate period %s, want more than zero", nanoredis.ErrInvalidArgument, r.Period)
	case r.Burst < 0:
		return fmt.Errorf("%w: rate burst %d, want zero or more", nanoredis.ErrInvalidArgument, r.Burst)
	case r.spacing() < 1:
		return fmt.Errorf("%w: rate %d per %s is more than one event per microsecond",
			nanoredis.ErrInvalidArgument, r.Limit, r.Period)
	}
	return nil
}

// Decision is the answer of a Limiter.
type Decision struct {
	// Allowed reports whether the events are admitted. Admitted events are
	// counted; refused events are not.
	Allowed bool
	// Remaining is how many more events the key may send now. It is zero
	// when Redis did not answer.
	Remaining int
	// RetryAfter is, for refused events, the wait until the same request
	// would be admitted. It is zero for admitted events and when Redis did
	// not answer.
	RetryAfter time.Duration
	// ResetAfter is the wait until the key has its full Burst again. It is
	// zero when Redis did not answer.
	ResetAfter time.Duration
}

// Limiter admits events of each key at a Rate shared by every replica.
type Limiter interface {
	// Allow is AllowN of one event.
	Allow(ctx context.Context, key string) (Decision, error)
	// AllowN admits n events of key at once, or none of them. When Redis does
	// not answer, it returns the answer of the FailurePolicy and an error
	// that wraps nanoredis.ErrUnavailable. An invalid key or n, including n
	// above the Burst, returns a refusal and an error that wraps
	// nanoredis.ErrInvalidArgument.
	AllowN(ctx context.Context, key string, n int) (Decision, error)
}

// New returns the limiter of rate for the keys under name, over the Redis
// client of store. The no-op store has no client: every call then returns
// the answer of the FailurePolicy. Two limiters with the same name share
// their counts, so give each limit its own name. A name is non-empty,
// without a glob character or a trailing ':'.
func New(store cache.Store, name string, rate Rate, opts ...nanoredis.Option) (Limiter, error) {
	return NewWithClient(guard.ClientOf(store), name, rate, opts...)
}

// NewWithClient is New over a client that the service owns. A nil client
// means no Redis.
func NewWithClient(client *redis.Client, name string, rate Rate, opts ...nanoredis.Option) (Limiter, error) {
	if err := rate.validate(); err != nil {
		return nil, err
	}
	runner, err := guard.NewRunner(client, name, opts)
	if err != nil {
		return nil, err
	}
	return &limiter{runner: runner, rate: rate}, nil
}

type limiter struct {
	runner *guard.Runner
	rate   Rate
}

func (l *limiter) Allow(ctx context.Context, key string) (Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *limiter) AllowN(ctx context.Context, key string, n int) (Decision, error) {
	redisKey, err := l.runner.Key(key)
	if err != nil {
		return Decision{}, err
	}
	if n < 1 || n > l.rate.burst() {
		return Decision{}, fmt.Errorf("%w: %d events, want 1 to the burst of %d",
			nanoredis.ErrInvalidArgument, n, l.rate.burst())
	}
	reply, err := l.runner.Eval(ctx, script, redisKey, replyLen, l.rate.spacing(), l.rate.burst(), n)
	if err != nil {
		return Decision{Allowed: l.runner.FailOpen()}, l.runner.Unavailable(ctx, err)
	}
	return Decision{
		Allowed:    reply[0] == 1,
		Remaining:  int(max(reply[1], 0)),
		RetryAfter: time.Duration(max(reply[2], 0)) * time.Microsecond,
		ResetAfter: time.Duration(max(reply[3], 0)) * time.Microsecond,
	}, nil
}

const replyLen = 4

// script keeps, for each key, the time at which the key will have its full
// burst again (its "full" time), in microseconds of the server clock. Each
// admitted event moves it one spacing later; it never lags the clock. Events
// are admitted while the full time stays within burst spacings of now. The
// key expires when it reaches its full time, so an idle key holds nothing in
// Redis.
//
// KEYS[1] the key. ARGV[1] spacing in microseconds, ARGV[2] burst, ARGV[3]
// events. Returns {admitted 0|1, remaining, retry after µs, reset after µs}.
// Times are written with %d: a Lua number of 16 digits would otherwise reach
// Redis in exponent form and lose its last digits.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var script = redis.NewScript(guard.ReplicateEffects + `
local spacing = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local events = tonumber(ARGV[3])
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000000 + tonumber(clock[2])
local window = spacing * burst

local full = tonumber(redis.call('GET', KEYS[1]))
if full == nil or full < now then
	full = now
end
local next_full = full + spacing * events
local earliest = next_full - window
if earliest > now then
	return {0, math.floor((window - (full - now)) / spacing), earliest - now, full - now}
end
local ttl_ms = math.max(1, math.ceil((next_full - now) / 1000))
redis.call('SET', KEYS[1], string.format('%d', next_full), 'PX', string.format('%d', ttl_ms))
return {1, math.floor((window - (next_full - now)) / spacing), 0, next_full - now}
`)
