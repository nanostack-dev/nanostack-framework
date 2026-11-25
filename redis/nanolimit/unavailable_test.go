package nanolimit_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// These tests need no server: a Redis that is down is a closed port, and the
// no-op cache store has no client at all.

// downClient is a client of a port that refuses connections.
func downClient(t *testing.T) *redis.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	rdb := redis.NewClient(
		&redis.Options{Addr: addr, MaxRetries: -1, DialerRetries: 1, DialTimeout: 200 * time.Millisecond},
	)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

type unavailableCase struct {
	name   string
	limits func(t *testing.T, logger *slog.Logger, opts ...nanolimit.Option) *nanolimit.Limits
}

func unavailableCases() []unavailableCase {
	return []unavailableCase{
		{"redis down", func(t *testing.T, logger *slog.Logger, opts ...nanolimit.Option) *nanolimit.Limits {
			return nanolimit.New(downClient(t), logger, opts...)
		}},
		{"nil client", func(_ *testing.T, logger *slog.Logger, opts ...nanolimit.Option) *nanolimit.Limits {
			return nanolimit.New(nil, logger, opts...)
		}},
		{"no-op store", func(_ *testing.T, logger *slog.Logger, opts ...nanolimit.Option) *nanolimit.Limits {
			if len(opts) > 0 {
				return nanolimit.New(nanocache.NoOp().RedisClient(), logger, opts...)
			}
			return nanolimit.NewFromStore(nanocache.NoOp(), logger)
		}},
	}
}

func TestFailOpenAdmitsAndWarnsOnce(t *testing.T) {
	for _, tc := range unavailableCases() {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			limits := tc.limits(t, slog.New(slog.NewTextHandler(&logs, nil)))
			ctx := t.Context()

			limiter := limits.RateLimiter("rate")
			for range 3 {
				if got, err := limiter.TryAcquire(ctx, 1); err != nil || !got {
					t.Fatalf("TryAcquire = %v, %v; want true, nil", got, err)
				}
			}
			if got, err := limiter.TryAcquireWait(ctx, 1, time.Second); err != nil || !got {
				t.Fatalf("TryAcquireWait = %v, %v; want true, nil", got, err)
			}
			if err := limiter.Acquire(ctx, 1); err != nil {
				t.Fatalf("Acquire = %v, want nil", err)
			}

			s := limits.Semaphore("sem")
			id, got, err := s.TryAcquire(ctx, time.Minute)
			if err != nil || !got || !nanolimit.IsStandIn(id) {
				t.Fatalf("TryAcquire = %q, %v, %v; want a stand-in permit", id, got, err)
			}
			if err = s.Release(ctx, id); err != nil {
				t.Fatalf("Release of a stand-in = %v, want nil", err)
			}
			ids, err := s.AcquireN(ctx, 2, time.Minute)
			if err != nil || len(ids) != 2 {
				t.Fatalf("AcquireN = %v, %v; want two stand-ins", ids, err)
			}
			if n, releaseErr := s.TryReleaseN(ctx, ids); releaseErr != nil || n != 2 {
				t.Fatalf("TryReleaseN of stand-ins = %d, %v; want 2, nil", n, releaseErr)
			}

			if n := strings.Count(logs.String(), "redis limit unavailable; failing open"); n != 1 {
				t.Fatalf("%d warnings, want 1 per interval:\n%s", n, logs.String())
			}
		})
	}
}

func TestFailClosedRefuses(t *testing.T) {
	for _, tc := range unavailableCases() {
		t.Run(tc.name, func(t *testing.T) {
			limits := tc.limits(t, discardLogger(), nanolimit.WithFailurePolicy(nanolimit.FailClosed))
			ctx := t.Context()
			if got, err := limits.RateLimiter("rate").
				TryAcquire(ctx, 1); got ||
				!errors.Is(err, nanolimit.ErrUnavailable) {
				t.Fatalf("TryAcquire = %v, %v; want false, ErrUnavailable", got, err)
			}
			if err := limits.RateLimiter("rate").Acquire(ctx, 1); !errors.Is(err, nanolimit.ErrUnavailable) {
				t.Fatalf("Acquire = %v, want ErrUnavailable", err)
			}
			if _, got, err := limits.Semaphore("sem").
				TryAcquire(ctx, time.Minute); got ||
				!errors.Is(err, nanolimit.ErrUnavailable) {
				t.Fatalf("Semaphore.TryAcquire = %v, %v; want false, ErrUnavailable", got, err)
			}
		})
	}
}

// Reads and configuration calls return the error under either policy: only
// an acquire has a safe default.
func TestReadsAndConfigurationReturnUnavailable(t *testing.T) {
	for _, tc := range unavailableCases() {
		t.Run(tc.name, func(t *testing.T) {
			limits := tc.limits(t, discardLogger())
			ctx := t.Context()
			limiter, s := limits.RateLimiter("rate"), limits.Semaphore("sem")
			for what, err := range map[string]error{
				"TrySetRate":       second(limiter.TrySetRate(ctx, nanolimit.RateConfig{Rate: 1, Interval: time.Second})),
				"SetRate":          limiter.SetRate(ctx, nanolimit.RateConfig{Rate: 1, Interval: time.Second}),
				"Config":           second(limiter.Config(ctx)),
				"AvailablePermits": second(limiter.AvailablePermits(ctx)),
				"Release":          limiter.Release(ctx, 1),
				"Delete":           second(limiter.Delete(ctx)),
				"Expire":           second(limiter.Expire(ctx, time.Second)),
				"TrySetPermits":    second(s.TrySetPermits(ctx, 1)),
				"Semaphore.Avail":  second(s.AvailablePermits(ctx)),
				"Semaphore.Rel":    s.Release(ctx, "0123456789abcdef0123456789abcdef"),
				"LeaseTime":        second(s.LeaseTime(ctx, "0123456789abcdef0123456789abcdef")),
			} {
				if !errors.Is(err, nanolimit.ErrUnavailable) {
					t.Errorf("%s = %v, want ErrUnavailable", what, err)
				}
			}
		})
	}
}

// An ended context is the caller's decision, not an outage: FailOpen does
// not admit it.
func TestEndedContextIsNotUnavailable(t *testing.T) {
	limits := nanolimit.New(downClient(t), discardLogger())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := limits.RateLimiter("rate").TryAcquire(ctx, 1)
	if got || !errors.Is(err, context.Canceled) || errors.Is(err, nanolimit.ErrUnavailable) {
		t.Fatalf("TryAcquire = %v, %v; want false, context.Canceled", got, err)
	}
	if _, got, err = limits.Semaphore("sem").TryAcquire(ctx, time.Minute); got || !errors.Is(err, context.Canceled) {
		t.Fatalf("Semaphore.TryAcquire = %v, %v; want false, context.Canceled", got, err)
	}
}

func TestInvalidArguments(t *testing.T) {
	limits := nanolimit.New(nil, discardLogger())
	ctx := t.Context()
	limiter := limits.RateLimiter("rate")
	for what, cfg := range map[string]nanolimit.RateConfig{
		"zero rate":       {Rate: 0, Interval: time.Second},
		"zero interval":   {Rate: 1},
		"short keepalive": {Rate: 1, Interval: time.Minute, KeepAlive: time.Second},
		"unknown type":    {Type: 7, Rate: 1, Interval: time.Second},
	} {
		if _, err := limiter.TrySetRate(ctx, cfg); !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Errorf("TrySetRate(%s) = %v, want ErrInvalidArgument", what, err)
		}
	}
	if _, err := limiter.TryAcquire(ctx, -1); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Errorf("TryAcquire(-1) = %v, want ErrInvalidArgument", err)
	}
	if _, err := limits.Semaphore("sem").TryAcquireN(ctx, -1, 0); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Errorf("Semaphore.TryAcquireN(-1) = %v, want ErrInvalidArgument", err)
	}
	if _, err := limits.Semaphore("sem").TryReleaseN(ctx, nil); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Errorf("Semaphore.TryReleaseN(nil) = %v, want ErrInvalidArgument", err)
	}
}

func second[T any](_ T, err error) error { return err }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
