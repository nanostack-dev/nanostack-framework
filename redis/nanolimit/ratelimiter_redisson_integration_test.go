//go:build integration

// Ported from Redisson's RedissonRateLimiterTest, Copyright (c) 2013-2026
// Nikita Koksharov, licensed under the Apache License, Version 2.0, at commit
// b019f0eb75d149a37711352db9db06f9973ca1eb. Nanostack changed it: Go
// tests, a namespace per test, and shorter loops and sleeps where a test
// says so. testExpire and testExpire2 use Expire; the async variants of
// testAcquireNegativePermits are the same Go calls.

package nanolimit_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

func overall(rate int64, interval time.Duration) nanolimit.RateConfig {
	return nanolimit.RateConfig{Type: nanolimit.Overall, Rate: rate, Interval: interval}
}

func perClient(rate int64, interval time.Duration) nanolimit.RateConfig {
	return nanolimit.RateConfig{Type: nanolimit.PerClient, Rate: rate, Interval: interval}
}

func TestRedissonRateLimiterKeepAliveTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testKeepAliveTime")
	cfg := overall(1, time.Second)
	cfg.KeepAlive = time.Second
	must(t, of(limiter.TrySetRate(ctx(t), cfg)))
	time.Sleep(1100 * time.Millisecond)
	eq(t, e.keyCount(), 0, "keys after the keep-alive")

	cfg = overall(10, 2*time.Second)
	cfg.KeepAlive = 2 * time.Second
	must(t, of(limiter.TrySetRate(ctx(t), cfg)))
	time.Sleep(time.Second)
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "TryAcquire")
	if ttl := must(t, of(e.rdb.PTTL(ctx(t), e.name("testKeepAliveTime")).Result())); ttl <= 1500*time.Millisecond {
		t.Fatalf("remainTimeToLive = %v, want > 1.5s: the acquire renews the keep-alive", ttl)
	}
}

// Redisson: expire 10s, sleep 12s. Shortened to 2s and 2.5s.
func TestRedissonRateLimiterExpire2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test1")
	must(t, of(limiter.TrySetRate(ctx(t), overall(5, 5*time.Second))))
	must(t, of(limiter.Expire(ctx(t), 2*time.Second)))
	ok(t, limiter.Acquire(ctx(t), 1))
	time.Sleep(2500 * time.Millisecond)
	eq(t, e.keyCount(), 0, "keys")
}

// Redisson: 200 threads, 20 samples, at least 16 at the rate. Shortened to
// 50 goroutines, 8 samples, at least 5.
func TestRedissonRateLimiterRateValue(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const rate = 10_000
	limiter := e.rate("test1")
	ok(t, limiter.SetRate(ctx(t), overall(rate, 10*time.Second)))
	acquireCtx, cancel := context.WithCancel(ctx(t))
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for {
				if err := limiter.Acquire(acquireCtx, 1); err != nil {
					return // The test cancelled acquireCtx.
				}
			}
		})
	}
	var sizes []int64
	for range 8 {
		sizes = append(sizes, must(t, of(e.rdb.ZCard(ctx(t), "{"+e.name("test1")+"}:permits").Result())))
		time.Sleep(time.Second)
	}
	cancel()
	wg.Wait()
	atRate := 0
	for _, size := range sizes {
		if size == rate {
			atRate++
		}
	}
	if atRate < 5 {
		t.Fatalf("permit set sizes %v: %d samples at the rate, want >= 5", sizes, atRate)
	}
}

func TestRedissonRateLimiterExpire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("limiter")
	must(t, of(limiter.TrySetRate(ctx(t), overall(2, 5*time.Second))))
	must(t, of(limiter.TryAcquire(ctx(t), 1)))
	must(t, of(limiter.Expire(ctx(t), time.Second)))
	time.Sleep(1100 * time.Millisecond)
	eq(t, e.keyCount(), 0, "keys")
}

func TestRedissonRateLimiterAcquisitionInterval(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("acquire")
	must(t, of(limiter.TrySetRate(ctx(t), overall(2, 5*time.Second))))
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "first")
	time.Sleep(4 * time.Second)
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "second")
	time.Sleep(1050 * time.Millisecond)
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "third: the first came back")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "fourth")
}

func TestRedissonRateLimiterRateConfig(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("acquire")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(1, 5*time.Second)))), true, "TrySetRate")
	cfg := must(t, of(limiter.Config(ctx(t))))
	eq(t, cfg.Rate, 1, "rate")
	eq(t, cfg.Interval, 5*time.Second, "interval")
	eq(t, cfg.Type, nanolimit.Overall, "type")
}

func TestRedissonRateLimiterAvailablePermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("rt2")
	must(t, of(limiter.TrySetRate(ctx(t), overall(10, 5*time.Second))))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available")
	ok(t, limiter.Acquire(ctx(t), 1))
	time.Sleep(6 * time.Second)
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available after the interval")
}

func TestRedissonRateLimiterPermitsExceeding(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("myLimiter")
	must(t, of(limiter.TrySetRate(ctx(t), perClient(1, time.Second))))
	if _, err := limiter.TryAcquire(ctx(t), 20); !errors.Is(err, nanolimit.ErrPermitsExceedRate) {
		t.Fatalf("TryAcquire(20) = %v, want ErrPermitsExceedRate", err)
	}
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "TryAcquire(1)")
}

func TestRedissonRateLimiterZeroTimeout(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("myLimiter")
	must(t, of(limiter.TrySetRate(ctx(t), overall(5, time.Second))))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 5, "available")
	try := func(want bool) {
		t.Helper()
		eq(t, must(t, of(limiter.TryAcquireWait(ctx(t), 1, 0))), want, "TryAcquireWait(1, 0)")
	}
	try(true)
	try(true)
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 3, "available")
	try(true)
	try(true)
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 1, "available")
	try(true)
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 0, "available")
	for range 5 {
		try(false)
	}
	time.Sleep(time.Second)
	for range 5 {
		try(true)
	}
	for range 5 {
		try(false)
	}
}

func TestRedissonRateLimiterTryAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("acquire")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(1, 5*time.Second)))), true, "TrySetRate")
	start := time.Now()
	eq(t, must(t, of(limiter.TryAcquireWait(ctx(t), 1, time.Second))), true, "first")
	eq(t, must(t, of(limiter.TryAcquireWait(ctx(t), 1, time.Second))), false, "second")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "third")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v, Redisson's timeout is 2s", elapsed)
	}
}

func TestRedissonRateLimiterAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("acquire")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(1, 5*time.Second)))), true, "TrySetRate")
	// Redisson acquires 10 times: 45s. Shortened to 3 (10s).
	for range 3 {
		ok(t, limiter.Acquire(ctx(t), 1))
	}
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "TryAcquire")
}

func TestRedissonRateLimiter(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(10, time.Second)))), true, "TrySetRate")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(20, time.Second)))), false, "second TrySetRate")
	for range 3 {
		for range 10 {
			eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "within the rate")
		}
		for range 10 {
			eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "over the rate")
		}
		time.Sleep(1050 * time.Millisecond)
	}
}

func TestRedissonRateLimiterRemove(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test")
	eq(t, must(t, of(limiter.Delete(ctx(t)))), false, "Delete of nothing")
	must(t, of(limiter.TrySetRate(ctx(t), overall(5, 5*time.Minute))))
	eq(t, e.keyCount(), 1, "keys")
	must(t, of(limiter.TryAcquire(ctx(t), 1)))
	deleted := must(t, of(limiter.Delete(ctx(t))))
	eq(t, e.keyCount(), 0, "keys after Delete")
	eq(t, deleted, true, "Delete")
}

// checkWindows is the assertion of testConcurrency2: in each window of a
// second after the first, no more than rate acquires.
func checkWindows(t *testing.T, stamps []time.Time, rate int) {
	t.Helper()
	slices.SortFunc(stamps, func(a, b time.Time) int { return a.Compare(b) })
	count, skip := 0, true
	var start time.Time
	for _, stamp := range stamps {
		if start.IsZero() {
			start = stamp
		}
		count++
		if stamp.Sub(start) >= time.Second {
			if !skip && count > rate {
				t.Fatalf("%d acquires in a window of a second, rate %d", count, rate)
			}
			skip = false
			start, count = time.Time{}, 0
		}
	}
}

// Redisson: 1000 acquires (55s). Shortened to 100.
func TestRedissonRateLimiterConcurrency2(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test")
	must(t, of(limiter.TrySetRate(ctx(t), overall(18, time.Second))))
	var (
		mu      sync.Mutex
		stamps  []time.Time
		counter atomic.Int64
		wg      sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			for {
				if err := limiter.Acquire(ctx(t), 1); err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				mu.Lock()
				stamps = append(stamps, time.Now())
				mu.Unlock()
				if counter.Add(1) > 100 {
					return
				}
			}
		})
	}
	wg.Wait()
	checkWindows(t, stamps, 18)
}

// Redisson: 500 acquires (50s). Shortened to 60.
func TestRedissonRateLimiterConcurrency(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(10, time.Second)))), true, "TrySetRate")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(20, time.Second)))), false, "second TrySetRate")
	var (
		mu      sync.Mutex
		stamps  []time.Time
		counter atomic.Int64
		wg      sync.WaitGroup
	)
	for i := range 8 {
		wg.Go(func() {
			for {
				acquired, err := limiter.TryAcquire(ctx(t), 1)
				if err != nil {
					t.Errorf("TryAcquire: %v", err)
					return
				}
				if acquired {
					if counter.Add(1) > 60 {
						return
					}
					mu.Lock()
					stamps = append(stamps, time.Now())
					mu.Unlock()
				}
				time.Sleep(time.Duration((i*7+int(counter.Load()))%10) * time.Millisecond)
			}
		})
	}
	wg.Wait()
	slices.SortFunc(stamps, func(a, b time.Time) int { return a.Compare(b) })
	var start time.Time
	for i, stamp := range stamps {
		if i%10 == 0 {
			if !start.IsZero() && stamp.Sub(start) <= 940*time.Millisecond {
				t.Fatalf("acquires %d and %d are %v apart, want > 940ms", i-10, i, stamp.Sub(start))
			}
			start = stamp
		}
	}
}

func TestRedissonRateLimiterRelease(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test_release")
	for _, cfg := range []func(int64, time.Duration) nanolimit.RateConfig{perClient, overall} {
		ok(t, limiter.SetRate(ctx(t), cfg(10, time.Second)))
		eq(t, must(t, of(limiter.Config(ctx(t)))).Rate, 10, "rate")
		ok(t, limiter.Acquire(ctx(t), 3))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available after acquire 3")
		ok(t, limiter.Release(ctx(t), 3))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available after release 3")
		ok(t, limiter.Release(ctx(t), 10))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available after release 10: capped")
		ok(t, limiter.Release(ctx(t), 0))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available after release 0")
		if err := limiter.Release(ctx(t), -1); !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Fatalf("Release(-1) = %v, want ErrInvalidArgument", err)
		}
	}
}

func TestRedissonRateLimiterAcquireNegativePermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test_acquire_negative")
	ok(t, limiter.SetRate(ctx(t), overall(5, 10*time.Second)))
	for what, err := range map[string]error{
		"TryAcquire":     second(limiter.TryAcquire(ctx(t), -1)),
		"TryAcquireWait": second(limiter.TryAcquireWait(ctx(t), -1, time.Second)),
		"Acquire":        limiter.Acquire(ctx(t), -1),
	} {
		if !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Fatalf("%s(-1) = %v, want ErrInvalidArgument", what, err)
		}
	}
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 5, "available")
}

func TestRedissonRateLimiterReleasedPermitsPreservedAfterExpiry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testNoCollapse")
	must(t, of(limiter.TrySetRate(ctx(t), overall(10, time.Second))))
	must(t, of(limiter.TryAcquire(ctx(t), 1)))
	ok(t, limiter.Release(ctx(t), 1))
	time.Sleep(300 * time.Millisecond)
	for range 8 {
		must(t, of(limiter.TryAcquire(ctx(t), 1)))
		ok(t, limiter.Release(ctx(t), 1))
	}
	time.Sleep(900 * time.Millisecond)
	must(t, of(limiter.TryAcquire(ctx(t), 1)))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 9, "available")
}
