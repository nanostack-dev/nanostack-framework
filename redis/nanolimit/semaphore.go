package nanolimit

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Permit is the answer of Semaphore.Acquire.
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

// Semaphore leases up to a fixed number of permits for each key, shared by
// every replica. A permit is held until it is released or its lease ends,
// whichever comes first, so a caller that crashes frees its permit when the
// lease ends. Choose a lease longer than the work it guards, or Extend it.
type Semaphore interface {
	// Acquire leases one permit of key for lease, if one is free. When Redis
	// does not answer, it returns the answer of the FailurePolicy, without
	// an ID, and an error that wraps ErrUnavailable.
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

type semaphore struct {
	base

	permits int
}

func (s *semaphore) Acquire(ctx context.Context, key string, lease time.Duration) (Permit, error) {
	redisKey, err := s.redisKey(key)
	if err != nil {
		return Permit{}, err
	}
	if lease < time.Millisecond {
		return Permit{}, fmt.Errorf("%w: lease %s, want at least 1ms", ErrInvalidArgument, lease)
	}
	id := rand.Text()
	reply, err := s.eval(ctx, acquireScript, redisKey, s.permits, lease.Milliseconds(), id)
	if err == nil && len(reply) != acquireReplyLen {
		err = fmt.Errorf("acquire script returned %d values, want %d", len(reply), acquireReplyLen)
	}
	if err != nil {
		return Permit{Acquired: s.opts.policy == FailOpen}, s.unavailable(ctx, err)
	}
	if reply[0] != 1 {
		return Permit{RetryAfter: time.Duration(max(reply[2], 0)) * time.Millisecond}, nil
	}
	return Permit{Acquired: true, ID: id, Available: int(max(reply[1], 0))}, nil
}

func (s *semaphore) Release(ctx context.Context, key, permitID string) error {
	return s.change(ctx, releaseScript, key, permitID)
}

func (s *semaphore) Extend(ctx context.Context, key, permitID string, lease time.Duration) error {
	if lease < time.Millisecond {
		return fmt.Errorf("%w: lease %s, want at least 1ms", ErrInvalidArgument, lease)
	}
	return s.change(ctx, extendScript, key, permitID, lease.Milliseconds())
}

// change runs the release or extend script on one permit, which answers
// {1} when the permit was held and {0} otherwise.
func (s *semaphore) change(ctx context.Context, script *redis.Script, key, permitID string, args ...any) error {
	redisKey, err := s.redisKey(key)
	if err != nil {
		return err
	}
	if permitID == "" {
		return nil
	}
	reply, err := s.eval(ctx, script, redisKey, append([]any{permitID}, args...)...)
	if err == nil && len(reply) != 1 {
		err = fmt.Errorf("permit script returned %d values, want 1", len(reply))
	}
	if err != nil {
		return s.unavailable(ctx, err)
	}
	if reply[0] != 1 {
		return fmt.Errorf("%w: %s %s", ErrPermitNotHeld, redisKey, permitID)
	}
	return nil
}

func (s *semaphore) Available(ctx context.Context, key string) (int, error) {
	redisKey, err := s.redisKey(key)
	if err != nil {
		return 0, err
	}
	reply, err := s.eval(ctx, heldScript, redisKey)
	if err == nil && len(reply) != 1 {
		err = fmt.Errorf("held script returned %d values, want 1", len(reply))
	}
	if err != nil {
		return 0, s.unavailable(ctx, err)
	}
	return max(s.permits-int(reply[0]), 0), nil
}

const acquireReplyLen = 3

// The semaphore of a key is a sorted set of permit IDs, each scored by the
// end of its lease in milliseconds of the server clock. Every script first
// drops the permits whose lease has ended, so a lost permit comes back
// without a sweeper, then sets the key to expire with its longest lease, so
// a key whose permits all ended holds nothing in Redis.
//
// Scores and times are written with %d: see rateScript.
const permitPrelude = replicateEffects + `
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

// replicateEffects makes a server older than Redis 5 replicate the writes of
// a script rather than the script, which reads the clock. Newer servers
// always do.
const replicateEffects = `
if redis.replicate_commands then
	redis.replicate_commands()
end
`

// acquireScript: ARGV[1] permits, ARGV[2] lease in ms, ARGV[3] permit ID.
// Returns {acquired 0|1, available, retry after ms}.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var acquireScript = redis.NewScript(permitPrelude + `
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
var releaseScript = redis.NewScript(permitPrelude + `
local released = redis.call('ZREM', KEYS[1], ARGV[1])
expire_with_longest_lease()
return {released}
`)

// extendScript: ARGV[1] permit ID, ARGV[2] lease in ms. Returns {extended
// 0|1}.
//
//nolint:gochecknoglobals // A script is loaded once per server and shared.
var extendScript = redis.NewScript(permitPrelude + `
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
var heldScript = redis.NewScript(permitPrelude + `
return {redis.call('ZCARD', KEYS[1])}
`)
