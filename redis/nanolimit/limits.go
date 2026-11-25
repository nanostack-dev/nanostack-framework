// Copyright 2026 Nanostack.
//
// Parts of this package are ported from Redisson, Copyright (c) 2013-2026
// Nikita Koksharov, licensed under the Apache License, Version 2.0. See
// doc.go, NOTICE and LICENSE in this directory.

package nanolimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

// Module provides *Limits over the nanocache Store of the graph, with the
// default options: fail open, one warning per minute.
var Module = nano.Module("limit", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewFromStore),
)

var (
	// ErrUnavailable wraps every failure to reach Redis: no client, a closed
	// or unreachable server, a timeout of the client, a server error. With
	// FailOpen an acquire returns success instead, and only the reads and the
	// configuration calls return it.
	ErrUnavailable = errors.New("nanolimit: redis unavailable")
	// ErrNotInitialized is returned by a rate limiter whose rate was never
	// set, or whose keys expired (RateConfig.KeepAlive). Set the rate and try
	// again.
	ErrNotInitialized = errors.New("nanolimit: rate limiter is not initialized")
	// ErrPermitsExceedRate is returned for an acquire of more permits than
	// the rate allows in one interval: it can never succeed.
	ErrPermitsExceedRate = errors.New("nanolimit: requested permits exceed the rate")
	// ErrInvalidArgument is returned for a negative permit count, a rate or
	// an interval that is not positive, or a keep-alive shorter than the
	// interval.
	ErrInvalidArgument = errors.New("nanolimit: invalid argument")
	// ErrPermitNotFound is returned by Semaphore.Release and LeaseTime for a
	// permit that was released, expired or never acquired.
	ErrPermitNotFound = errors.New("nanolimit: permit released, expired or unknown")
)

// FailurePolicy decides what an acquire returns when Redis is unavailable.
type FailurePolicy int

const (
	// FailOpen admits the caller: RateLimiter.TryAcquire returns true and
	// Semaphore.TryAcquire returns a stand-in permit, whose release is a
	// no-op. A warning is logged at most once per warn interval. This is the
	// default: a limit protects a service, it must not take it down.
	FailOpen FailurePolicy = iota
	// FailClosed returns the error, wrapping ErrUnavailable, and refuses.
	FailClosed
)

const (
	defaultWarnInterval = time.Minute
	// releaseTimeout bounds the release of the permits of an acquire whose
	// reply was lost.
	releaseTimeout = 5 * time.Second
	// unavailablePermitPrefix marks a stand-in permit of FailOpen.
	unavailablePermitPrefix = "unavailable-"
	idBytes                 = 16
)

// Option configures Limits.
type Option func(*Limits)

// WithFailurePolicy sets what an acquire returns when Redis is unavailable.
// The default is FailOpen.
func WithFailurePolicy(policy FailurePolicy) Option {
	return func(l *Limits) { l.policy = policy }
}

// WithClientID sets the identity of this process for RateType PerClient.
// The default is a random ID per Limits, as Redisson uses one per client.
func WithClientID(id string) Option {
	return func(l *Limits) { l.clientID = id }
}

// WithWarnInterval spaces the warnings of FailOpen. The default is a minute.
func WithWarnInterval(interval time.Duration) Option {
	return func(l *Limits) { l.warnEvery = interval }
}

// Limits hands out the rate limiters and semaphores of one Redis client.
// A limiter or a semaphore is a name: getting one is free, and two values
// with the same name share their state, in this process and in every other.
type Limits struct {
	rdb       *redis.Client
	logger    *slog.Logger
	clientID  string
	policy    FailurePolicy
	warnEvery time.Duration

	mu         sync.Mutex
	lastWarn   time.Time
	suppressed int
}

// New returns the Limits over rdb. A nil rdb is a Redis that is always
// unavailable: the failure policy decides every acquire.
func New(rdb *redis.Client, logger *slog.Logger, opts ...Option) *Limits {
	if logger == nil {
		logger = slog.Default()
	}
	l := &Limits{
		rdb:       rdb,
		logger:    logger.With(slog.String(log.KeyComponent, "limit")),
		policy:    FailOpen,
		warnEvery: defaultWarnInterval,
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.clientID == "" {
		l.clientID = newID()
	}
	return l
}

// NewFromStore returns the Limits over the client of store. The no-op store
// has no client, so its Limits fail open.
func NewFromStore(store nanocache.Store, logger *slog.Logger) *Limits {
	var rdb *redis.Client
	if store != nil {
		rdb = store.RedisClient()
	}
	return New(rdb, logger)
}

// RateLimiter returns the rate limiter called name.
func (l *Limits) RateLimiter(name string) RateLimiter {
	return &rateLimiter{limits: l, name: name}
}

// Semaphore returns the permit-expirable semaphore called name.
func (l *Limits) Semaphore(name string) Semaphore {
	return &semaphore{
		limits:      l,
		name:        name,
		timeoutName: suffixName(name, "timeout"),
		channelName: prefixName("nanolimit_sc", name),
	}
}

// suffixName is Redisson's: every key of an object shares the hash slot of
// its name, so the scripts run on a cluster too.
func suffixName(name, suffix string) string {
	if strings.Contains(name, "{") {
		return name + ":" + suffix
	}
	return "{" + name + "}:" + suffix
}

func prefixName(prefix, name string) string {
	if strings.Contains(name, "{") {
		return prefix + ":" + name
	}
	return prefix + ":{" + name + "}"
}

// eval runs script. A nil reply is (nil, nil).
func (l *Limits) eval(ctx context.Context, script *redis.Script, keys []string, args ...any) (any, error) {
	if l.rdb == nil {
		return nil, fmt.Errorf("%w: no redis client", ErrUnavailable)
	}
	reply, err := script.Run(ctx, l.rdb, keys, args...).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, classify(ctx, err)
	}
	return reply, nil
}

// classify maps a failed command: the caller's cancellation, the assertions
// of the scripts, and everything else as ErrUnavailable.
func classify(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("nanolimit: %w", ctxErr)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "RateLimiter is not initialized"):
		return ErrNotInitialized
	case strings.Contains(msg, "Requested permits amount cannot exceed defined rate"):
		return ErrPermitsExceedRate
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// degrade applies the failure policy to the error of an acquire: nil means
// admit.
func (l *Limits) degrade(ctx context.Context, op, name string, err error) error {
	if l.policy != FailOpen || !errors.Is(err, ErrUnavailable) {
		return err
	}
	l.warn(ctx, op, name, err)
	return nil
}

func (l *Limits) warn(ctx context.Context, op, name string, err error) {
	l.mu.Lock()
	now := time.Now()
	if !l.lastWarn.IsZero() && now.Sub(l.lastWarn) < l.warnEvery {
		l.suppressed++
		l.mu.Unlock()
		return
	}
	suppressed := l.suppressed
	l.lastWarn, l.suppressed = now, 0
	l.mu.Unlock()
	l.logger.WarnContext(ctx, "redis limit unavailable; failing open",
		slog.String("op", op), slog.String("name", name), log.Err(err),
		slog.Int("suppressed_warnings", suppressed))
}

func newIDBytes() []byte {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b) // crypto/rand.Read never fails.
	return b
}

// newID is Redisson's generateId: 16 random bytes in hex.
func newID() string { return hex.EncodeToString(newIDBytes()) }

func nowMillis() int64 { return time.Now().UnixMilli() }

// toInt64 reads an integer reply, which a script returns as an integer or,
// when it returns a stored value, as a string.
func toInt64(reply any) (int64, error) {
	switch v := reply.(type) {
	case int64:
		return v, nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("nanolimit: parse reply %q: %w", v, err)
		}
		return n, nil
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("nanolimit: unexpected reply %T", reply)
	}
}

// expireScript is RedissonObject's expire of several keys: it returns 1 when
// any key got the timeout.
//
//nolint:gochecknoglobals // A script is compiled once and cached by its SHA.
var expireScript = redis.NewScript(`
local result = 0;
for j = 1, #KEYS do
  local expireSet = redis.call('pexpire', KEYS[j], ARGV[1]);
  if expireSet == 1 then
    result = expireSet;
  end;
end;
return result;`)

func (l *Limits) expire(ctx context.Context, ttl time.Duration, keys ...string) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("%w: ttl must be positive", ErrInvalidArgument)
	}
	reply, err := l.eval(ctx, expireScript, keys, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func (l *Limits) del(ctx context.Context, keys ...string) (bool, error) {
	if l.rdb == nil {
		return false, fmt.Errorf("%w: no redis client", ErrUnavailable)
	}
	n, err := l.rdb.Del(ctx, keys...).Result()
	if err != nil {
		return false, classify(ctx, err)
	}
	return n > 0, nil
}
