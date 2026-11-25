// Package semaphore leases up to N permits per key, shared by every replica
// of a service through Redis.
//
// A permit is held until it is released or its lease ends, whichever comes
// first, so a replica that crashes frees its permit when the lease ends:
//
//	runs, err := semaphore.New(store, "api:runs", 2)
//	permit, err := runs.Acquire(ctx, userID, 5*time.Minute)
//	if permit.Acquired {
//		defer runs.Release(context.WithoutCancel(ctx), userID, permit.ID)
//	}
//
// Each call is one atomic script on one key that reads the clock of the
// Redis server. Without Redis, Acquire returns the answer of the
// nanoredis.FailurePolicy with an error that wraps nanoredis.ErrUnavailable.
package semaphore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/internal/guard"
)

// ErrPermitNotHeld is returned by Release and Extend for a permit that is
// not held any more: released already, or its lease ended and the permit was
// reclaimed.
var ErrPermitNotHeld = errors.New("semaphore: permit not held")

// Permit is the answer of Pool.Acquire.
type Permit struct {
	// Acquired reports whether the caller holds a permit now.
	Acquired bool
	// ID names the permit for Release and Extend. It is empty when no permit
	// was leased, including a permit granted by FailOpen without Redis;
	// Release and Extend accept the empty ID and do nothing.
	ID string
	// Available is how many permits of the key are still free. It is zero
	// when Redis did not answer.
	Available int
	// RetryAfter is, when no permit was free, the wait until the earliest
	// lease ends. A release can free a permit sooner. It is zero otherwise.
	RetryAfter time.Duration
}

// Pool leases up to a fixed number of permits for each key. Choose a lease
// longer than the work it guards, or Extend it.
type Pool interface {
	// Acquire leases one permit of key for lease, if one is free. When Redis
	// does not answer, it returns the answer of the FailurePolicy, without
	// an ID, and an error that wraps nanoredis.ErrUnavailable.
	Acquire(ctx context.Context, key string, lease time.Duration) (Permit, error)
	// Release frees the permit. It returns ErrPermitNotHeld when the permit
	// was released already or its lease ended, so a late release never
	// frees a permit that another caller holds now.
	Release(ctx context.Context, key, permitID string) error
	// Extend restarts the lease of a held permit from now. It returns
	// ErrPermitNotHeld when the permit was released or its lease ended.
	Extend(ctx context.Context, key, permitID string, lease time.Duration) error
	// Available returns how many permits of key are free now. It is a
	// reading, not a decision: when Redis does not answer it returns the
	// error and no FailurePolicy applies.
	Available(ctx context.Context, key string) (int, error)
}

// New returns the pool of permits for each key under name, over the Redis
// client of store. The no-op store has no client: see Pool.Acquire. The
// count is checked on every call, so a new permits applies at once to the
// permits leased after it. A name is non-empty, without a glob character or
// a trailing ':'.
func New(store cache.Store, name string, permits int, opts ...nanoredis.Option) (Pool, error) {
	return NewWithClient(guard.ClientOf(store), name, permits, opts...)
}

// NewWithClient is New over a client that the service owns. A nil client
// means no Redis.
func NewWithClient(client *redis.Client, name string, permits int, opts ...nanoredis.Option) (Pool, error) {
	if permits < 1 {
		return nil, fmt.Errorf("%w: permits %d, want at least 1", nanoredis.ErrInvalidArgument, permits)
	}
	runner, err := guard.NewRunner(client, name, opts)
	if err != nil {
		return nil, err
	}
	return &pool{runner: runner, permits: permits}, nil
}

type pool struct {
	runner  *guard.Runner
	permits int
}

func (p *pool) Acquire(ctx context.Context, key string, lease time.Duration) (Permit, error) {
	redisKey, err := p.runner.Key(key)
	if err != nil {
		return Permit{}, err
	}
	if err = validLease(lease); err != nil {
		return Permit{}, err
	}
	id := rand.Text()
	reply, err := p.runner.Eval(ctx, acquireScript, redisKey, acquireReplyLen, p.permits, lease.Milliseconds(), id)
	if err != nil {
		return Permit{Acquired: p.runner.FailOpen()}, p.runner.Unavailable(ctx, err)
	}
	if reply[0] != 1 {
		return Permit{RetryAfter: time.Duration(max(reply[2], 0)) * time.Millisecond}, nil
	}
	return Permit{Acquired: true, ID: id, Available: int(max(reply[1], 0))}, nil
}

func (p *pool) Release(ctx context.Context, key, permitID string) error {
	return p.change(ctx, releaseScript, key, permitID)
}

func (p *pool) Extend(ctx context.Context, key, permitID string, lease time.Duration) error {
	if err := validLease(lease); err != nil {
		return err
	}
	return p.change(ctx, extendScript, key, permitID, lease.Milliseconds())
}

// change runs the release or extend script on one permit, which answers
// {1} when the permit was held and {0} otherwise.
func (p *pool) change(ctx context.Context, script *redis.Script, key, permitID string, args ...any) error {
	redisKey, err := p.runner.Key(key)
	if err != nil {
		return err
	}
	if permitID == "" {
		return nil
	}
	reply, err := p.runner.Eval(ctx, script, redisKey, 1, append([]any{permitID}, args...)...)
	if err != nil {
		return p.runner.Unavailable(ctx, err)
	}
	if reply[0] != 1 {
		return fmt.Errorf("%w: %s %s", ErrPermitNotHeld, redisKey, permitID)
	}
	return nil
}

func (p *pool) Available(ctx context.Context, key string) (int, error) {
	redisKey, err := p.runner.Key(key)
	if err != nil {
		return 0, err
	}
	reply, err := p.runner.Eval(ctx, heldScript, redisKey, 1)
	if err != nil {
		return 0, p.runner.Unavailable(ctx, err)
	}
	return max(p.permits-int(reply[0]), 0), nil
}

func validLease(lease time.Duration) error {
	if lease < time.Millisecond {
		return fmt.Errorf("%w: lease %s, want at least 1ms", nanoredis.ErrInvalidArgument, lease)
	}
	return nil
}

const acquireReplyLen = 3

// The pool of a key is a sorted set of permit IDs, each scored by the end of
// its lease in milliseconds of the server clock. Every script first drops
// the permits whose lease has ended, so a lost permit comes back without a
// sweeper, then sets the key to expire with its longest lease, so a key
// whose permits all ended holds nothing in Redis.
//
// Scores and times are written with %d: a Lua number of 13 digits or more
// can otherwise reach Redis in exponent form.
const prelude = guard.ReplicateEffects + `
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', string.format('%d', now))
local function expire_with_longest_lease()
	local last = redis.call('ZRANGE', KEYS[1], -1, -1, 'WITHSCORES')
	if last[2] then
		redis.call('PEXPIRE', KEYS[1], string.format('%d', math.max(1, tonumber(last[2]) - now)))
	else
		redis.call('DEL', KEYS[1])
	end
end
`

// acquireScript: ARGV[1] permits, ARGV[2] lease in ms, ARGV[3] permit ID.
// Returns {acquired 0|1, available, retry after ms}.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var acquireScript = redis.NewScript(prelude + `
local permits = tonumber(ARGV[1])
local held = redis.call('ZCARD', KEYS[1])
if held >= permits then
	local first = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
	local wait = 0
	if first[2] then
		wait = tonumber(first[2]) - now
	end
	return {0, 0, wait}
end
redis.call('ZADD', KEYS[1], string.format('%d', now + tonumber(ARGV[2])), ARGV[3])
expire_with_longest_lease()
return {1, permits - held - 1, 0}
`)

// releaseScript: ARGV[1] permit ID. Returns {released 0|1}.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var releaseScript = redis.NewScript(prelude + `
local released = redis.call('ZREM', KEYS[1], ARGV[1])
expire_with_longest_lease()
return {released}
`)

// extendScript: ARGV[1] permit ID, ARGV[2] lease in ms. Returns {extended
// 0|1}.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var extendScript = redis.NewScript(prelude + `
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then
	return {0}
end
redis.call('ZADD', KEYS[1], 'XX', string.format('%d', now + tonumber(ARGV[2])), ARGV[1])
expire_with_longest_lease()
return {1}
`)

// heldScript returns {held}: the permits whose lease has not ended.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var heldScript = redis.NewScript(prelude + `
return {redis.call('ZCARD', KEYS[1])}
`)
