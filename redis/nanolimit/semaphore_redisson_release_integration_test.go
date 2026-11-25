//go:build integration

// Ported from Redisson's RedissonPermitExpirableSemaphoreTest (the release
// and lease cases), Copyright (c) 2013-2026 Nikita Koksharov, licensed under
// the Apache License, Version 2.0, at commit
// b019f0eb75d149a37711352db9db06f9973ca1eb. Nanostack changed it: Go tests
// and a namespace per test.

package nanolimit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

func isNotFound(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, nanolimit.ErrPermitNotFound) {
		t.Fatalf("%s = %v, want ErrPermitNotFound", what, err)
	}
}

func TestRedissonSemaphoreUpdateLeaseTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	eq(t, must(t, of(s.UpdateLeaseTime(ctx(t), "1234", time.Second))), false, "UpdateLeaseTime of an unknown permit")
	id := must(t, of(s.Acquire(ctx(t), nanolimit.NoExpiry)))
	eq(t, must(t, of(s.UpdateLeaseTime(ctx(t), id, time.Second))), true, "UpdateLeaseTime")
	time.Sleep(1200 * ms)
	eq(t, must(t, of(s.UpdateLeaseTime(ctx(t), id, time.Second))), false, "UpdateLeaseTime of an expired permit")
	id2 := tryAcquireWaitLease(t, s, time.Second, time.Second)
	eq(t, must(t, of(s.UpdateLeaseTime(ctx(t), id2, 3*time.Second))), true, "UpdateLeaseTime")
	time.Sleep(2800 * ms)
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 0, "available")
	time.Sleep(500 * ms)
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 1, "available")
	eq(t, must(t, of(s.UpdateLeaseTime(ctx(t), id2, 2*time.Second))), false, "UpdateLeaseTime of an expired permit")
}

func TestRedissonSemaphoreReleaseWithoutPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	isNotFound(t, e.sem("test").Release(ctx(t), "1234"), "Release")
}

func TestRedissonSemaphoreReleaseExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	id := tryAcquireWaitLease(t, s, 100*ms, 100*ms)
	time.Sleep(200 * ms)
	eq(t, must(t, of(s.TryRelease(ctx(t), id))), false, "TryRelease of an expired permit")
}

func TestRedissonSemaphoreRelease(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	ids := must(t, of(s.AcquireN(ctx(t), 6, nanolimit.NoExpiry)))
	eq(t, len(ids), 6, "acquired")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	ok(t, s.ReleaseN(ctx(t), ids[:4]))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 8, "available")
	ok(t, s.ReleaseN(ctx(t), ids[4:]))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
	isNotFound(t, s.ReleaseN(ctx(t), ids), "ReleaseN of released permits")
}

func TestRedissonSemaphoreReleaseAsyncMany(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	ids := must(t, of(s.AcquireN(ctx(t), 6, nanolimit.NoExpiry)))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	eq(t, must(t, of(s.TryReleaseN(ctx(t), ids[:4]))), 4, "TryReleaseN")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 8, "available")
	eq(t, must(t, of(s.TryReleaseN(ctx(t), ids[4:]))), 2, "TryReleaseN")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonSemaphoreReleaseManyExpiredDoesNotThrow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	ids := must(t, of(s.AcquireN(ctx(t), 6, 100*ms)))
	eq(t, len(ids), 6, "acquired")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	time.Sleep(250 * ms)
	must(t, of(s.Acquire(ctx(t), 100*ms)))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 9, "available")
	eventually(t, 250*ms, func() bool { return must(t, of(s.AvailablePermits(ctx(t)))) == 10 }, "all permits back")
}

func TestRedissonSemaphoreTryReleaseManyExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	timed := must(t, of(s.TryAcquireNWait(ctx(t), 3, 100*ms, 100*ms)))
	infinite := must(t, of(s.TryAcquireN(ctx(t), 3, nanolimit.NoExpiry)))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	time.Sleep(200 * ms)
	eq(t, must(t, of(s.TryReleaseN(ctx(t), infinite))), 3, "TryReleaseN of held permits")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
	eq(t, must(t, of(s.TryReleaseN(ctx(t), timed))), 0, "TryReleaseN of expired permits")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonSemaphoreReleaseManyNotExistOrExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	timed := must(t, of(s.AcquireN(ctx(t), 2, 100*ms)))
	ids := must(t, of(s.TryAcquireN(ctx(t), 8, nanolimit.NoExpiry)))
	eq(t, must(t, of(s.TryReleaseN(ctx(t), ids[:2]))), 2, "TryReleaseN")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 2, "available")
	time.Sleep(100 * ms)
	isNotFound(t, s.ReleaseN(ctx(t), timed), "ReleaseN of expired permits")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available: the expired permits are reclaimed")
	isNotFound(
		t,
		s.ReleaseN(ctx(t), append(append([]string{}, ids[2:4]...), ids[:2]...)),
		"ReleaseN with released permits",
	)
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	isNotFound(t, s.ReleaseN(ctx(t), []string{"1234"}), "ReleaseN of an unknown permit")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	isNotFound(
		t,
		s.ReleaseN(ctx(t), append(append([]string{}, ids[4:6]...), "4567")),
		"ReleaseN with an unknown permit",
	)
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
}

func TestRedissonSemaphoreGetLeaseTime(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	_, err := s.LeaseTime(ctx(t), "1234")
	isNotFound(t, err, "LeaseTime of an unknown permit")
	id := must(t, of(s.Acquire(ctx(t), nanolimit.NoExpiry)))
	eq(t, must(t, of(s.LeaseTime(ctx(t), id))), nanolimit.NoExpiry, "LeaseTime")
	ok(t, s.Release(ctx(t), id))
	id = must(t, of(s.Acquire(ctx(t), 1100*ms)))
	time.Sleep(100 * ms)
	if lease := must(t, of(s.LeaseTime(ctx(t), id))); lease > time.Second {
		t.Fatalf("LeaseTime = %v, want <= 1s", lease)
	}
	eventually(t, 1100*ms, func() bool { return must(t, of(s.AvailablePermits(ctx(t)))) == 1 }, "the lease ends")
}
