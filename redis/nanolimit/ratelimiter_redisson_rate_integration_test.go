//go:build integration

// Ported from Redisson's RedissonRateLimiterTest (the setRate and updateRate
// cases), Copyright (c) 2013-2026 Nikita Koksharov, licensed under the Apache
// License, Version 2.0, at commit b019f0eb75d149a37711352db9db06f9973ca1eb.
// Nanostack changed it: Go tests and a namespace per test.

package nanolimit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

func keepState(cfg nanolimit.RateConfig) nanolimit.RateConfig {
	cfg.KeepState = true
	return cfg
}

func TestRedissonRateLimiterUpdateRateConfig(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("acquire")
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(1, 5*time.Second)))), true, "TrySetRate")
	ok(t, limiter.SetRate(ctx(t), overall(2, 5*time.Second)))
	cfg := must(t, of(limiter.Config(ctx(t))))
	eq(t, cfg.Rate, 2, "rate")
	eq(t, cfg.Interval, 5*time.Second, "interval")
	eq(t, cfg.Type, nanolimit.Overall, "type")
}

func TestRedissonRateLimiterChangeRate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("test_change_rate")
	for _, cfg := range []func(int64, time.Duration) nanolimit.RateConfig{perClient, overall} {
		ok(t, limiter.SetRate(ctx(t), cfg(10, time.Second)))
		eq(t, must(t, of(limiter.Config(ctx(t)))).Rate, 10, "rate")
		ok(t, limiter.Acquire(ctx(t), 1))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 9, "available")
		ok(t, limiter.SetRate(ctx(t), cfg(20, time.Second)))
		eq(t, must(t, of(limiter.Config(ctx(t)))).Rate, 20, "rate")
		ok(t, limiter.Acquire(ctx(t), 1))
		eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 19, "available")
	}
}

func TestRedissonRateLimiterSetStateArgs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testSetStateArgs")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, time.Second))))
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	time.Sleep(1100 * time.Millisecond)
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
}

func TestRedissonRateLimiterSetStateArgsKeepState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testSetStateArgsKeepState")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 5*time.Second))))
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(20, 5*time.Second))))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 17, "available")
}

func TestRedissonRateLimiterSetStateArgsNotKeepState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testSetStateArgsNotKeepState")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 5*time.Second))))
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	ok(t, limiter.SetRate(ctx(t), overall(20, 5*time.Second)))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 20, "available")
}

func TestRedissonRateLimiterUpdateRateNonExisting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateNonExisting")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(10, 5*time.Second))))), false, "UpdateRate")
	eq(t, e.keyCount(), 0, "keys")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(perClient(10, 5*time.Second))))), false, "UpdateRate")
	eq(t, e.keyCount(), 0, "keys")
}

func TestRedissonRateLimiterUpdateRateKeepsState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateKeepsState")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 5*time.Second))))
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(10, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available")
	ok(t, limiter.Acquire(ctx(t), 2))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 8, "available")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 10))), false, "TryAcquire(10)")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 8))), true, "TryAcquire(8)")
}

func TestRedissonRateLimiterUpdateRateHigherRate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateExistingKeyHigherRate")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 2*time.Second))))
	time.Sleep(time.Second)
	ok(t, limiter.Acquire(ctx(t), 4))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 6, "available")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(20, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 16, "available: 20 - 4 used")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 16))), true, "TryAcquire(16)")
}

func TestRedissonRateLimiterUpdateRateLowerRateNewValuePositive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateLowerRateNewValuePositive")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 2*time.Second))))
	time.Sleep(time.Second)
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(5, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 2, "available: 5 - 3 used")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 3))), false, "TryAcquire(3)")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 2))), true, "TryAcquire(2)")
}

func TestRedissonRateLimiterUpdateRateLowerRateNewValueNegative(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateLowerRateNewValueNegative")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 2*time.Second))))
	time.Sleep(time.Second)
	ok(t, limiter.Acquire(ctx(t), 8))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 2, "available")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(5, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 0, "available: 5 - 8 used, floored")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "TryAcquire(1)")
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 2))), false, "TryAcquire(2)")
}

func TestRedissonRateLimiterUpdateRatePermitResetFull(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRatePermitResetFull")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, time.Second))))
	ok(t, limiter.Acquire(ctx(t), 8))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 2, "available")
	time.Sleep(2 * time.Second)
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(10, time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonRateLimiterUpdateRateModeChangeClearsState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateModeChangeClearsState")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 5*time.Second))))
	ok(t, limiter.Acquire(ctx(t), 4))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 6, "available")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(perClient(10, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonRateLimiterUpdateRateKeepAliveTimeLessThanIntervalThrows(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateKeepAliveTimeLessThanIntervalThrows")
	cfg := keepState(overall(10, 5*time.Second))
	cfg.KeepAlive = time.Second
	if _, err := limiter.UpdateRate(ctx(t), cfg); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Fatalf("UpdateRate = %v, want ErrInvalidArgument", err)
	}
}

func TestRedissonRateLimiterUpdateRateRateIntervalChangeDropsExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRateRateIntervalChangeDropsExpired")
	ok(t, limiter.SetRate(ctx(t), keepState(overall(10, 5*time.Second))))
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	time.Sleep(1100 * time.Millisecond)
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(overall(10, time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonRateLimiterUpdateRatePerClientUsesClientState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("testUpdateRatePerClientUsesClientState")
	ok(t, limiter.SetRate(ctx(t), keepState(perClient(10, 5*time.Second))))
	ok(t, limiter.Acquire(ctx(t), 3))
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 7, "available")
	eq(t, must(t, of(limiter.UpdateRate(ctx(t), keepState(perClient(20, 5*time.Second))))), true, "UpdateRate")
	eq(t, must(t, of(limiter.AvailablePermits(ctx(t)))), 17, "available")
}
