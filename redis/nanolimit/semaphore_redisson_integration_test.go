//go:build integration

// Ported from Redisson's RedissonPermitExpirableSemaphoreTest, Copyright (c)
// 2013-2026 Nikita Koksharov, licensed under the Apache License, Version
// 2.0, at commit b019f0eb75d149a37711352db9db06f9973ca1eb. Nanostack changed
// it: Go tests, a namespace per test, and shorter loops where a test says
// so. Not ported: testGetInClusterNameMapper (no cluster, no name mapper in
// this port). The async tests are the same Go calls, which take a context.

package nanolimit_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

const ms = time.Millisecond

// tryAcquireWaitLease is Redisson's tryAcquire(waitTime, leaseTime): an ID
// or "".
func tryAcquireWaitLease(t *testing.T, s nanolimit.Semaphore, wait, lease time.Duration) string {
	t.Helper()
	id, _, err := s.TryAcquireWait(ctx(t), wait, lease)
	ok(t, err)
	return id
}

func TestRedissonSemaphoreNotExistent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	eq(t, must(t, of(e.sem("testSemaphoreForNPE").AvailablePermits(ctx(t)))), 0, "available")
}

// acquireThree is the common part of testAvailablePermits,
// testAcquiredPermits and testGetPermits: two permits of 1s, a third
// refused, then a fourth after the first two expired.
func acquireThree(t *testing.T, s nanolimit.Semaphore, check func(stage string)) {
	t.Helper()
	eq(t, must(t, of(s.TrySetPermits(ctx(t), 2))), true, "TrySetPermits")
	check("set")
	if tryAcquireWaitLease(t, s, 200*ms, time.Second) == "" {
		t.Fatal("acquire 1 failed")
	}
	check("one")
	if tryAcquireWaitLease(t, s, 200*ms, time.Second) == "" {
		t.Fatal("acquire 2 failed")
	}
	if tryAcquireWaitLease(t, s, 200*ms, time.Second) != "" {
		t.Fatal("acquire 3 succeeded")
	}
	check("full")
	time.Sleep(1100 * ms)
	if tryAcquireWaitLease(t, s, 200*ms, time.Second) == "" {
		t.Fatal("acquire 4 failed")
	}
	time.Sleep(1100 * ms)
	check("expired")
}

func TestRedissonSemaphoreAvailablePermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test-semaphore")
	want := map[string]int64{"set": 2, "full": 0, "expired": 2}
	acquireThree(t, s, func(stage string) {
		if w, checked := want[stage]; checked {
			eq(t, must(t, of(s.AvailablePermits(ctx(t)))), w, "available at "+stage)
		}
	})
}

func TestRedissonSemaphoreAcquiredPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test-semaphore")
	want := map[string]int64{"set": 0, "one": 1, "full": 2, "expired": 0}
	acquireThree(t, s, func(stage string) {
		eq(t, must(t, of(s.AcquiredPermits(ctx(t)))), want[stage], "acquired at "+stage)
	})
}

func TestRedissonSemaphoreGetPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test-semaphore")
	want := map[string]int64{"set": 2, "full": 2, "expired": 2}
	acquireThree(t, s, func(stage string) {
		if w, checked := want[stage]; checked {
			eq(t, must(t, of(s.Permits(ctx(t)))), w, "permits at "+stage)
		}
	})
}

func TestRedissonSemaphoreExpiration(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("some-key")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	must(t, of(s.Expire(ctx(t), 3*time.Second)))
	tryAcquireWaitLease(t, s, time.Second, time.Second)
	time.Sleep(4100 * ms)
	eq(t, e.keyCount(), 0, "keys")
}

// expireWaiter is testExpire and testExpireTryAcquire: a permit of 2s, and
// another client whose Acquire gets it when the lease ends.
func expireWaiter(t *testing.T, e *env, permitID string) {
	t.Helper()
	start := time.Now()
	other := e.client().Semaphore(e.name("test"))
	id := must(t, of(other.Acquire(ctx(t), nanolimit.NoExpiry)))
	between(t, time.Since(start), 1900*ms, 2100*ms, "wait for the expired lease")
	ok(t, other.Release(ctx(t), id))
	eq(t, must(t, of(e.sem("test").TryRelease(ctx(t), permitID))), false, "TryRelease of the expired permit")
}

func TestRedissonSemaphoreExpire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	expireWaiter(t, e, must(t, of(s.Acquire(ctx(t), 2*time.Second))))
}

func TestRedissonSemaphoreExpireTryAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	expireWaiter(t, e, tryAcquireWaitLease(t, s, 100*ms, 2*time.Second))
}

func TestRedissonSemaphoreTrySetPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	eq(t, must(t, of(s.TrySetPermits(ctx(t), 10))), true, "TrySetPermits")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
	eq(t, must(t, of(s.TrySetPermits(ctx(t), 15))), false, "second TrySetPermits")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonSemaphoreSetPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	counts := func(permits, available, acquired int64) {
		t.Helper()
		eq(t, must(t, of(s.Permits(ctx(t)))), permits, "permits")
		eq(t, must(t, of(s.AvailablePermits(ctx(t)))), available, "available")
		eq(t, must(t, of(s.AcquiredPermits(ctx(t)))), acquired, "acquired")
	}
	ok(t, s.SetPermits(ctx(t), 10))
	counts(10, 10, 0)
	eq(t, must(t, of(s.TrySetPermits(ctx(t), 15))), false, "TrySetPermits")
	counts(10, 10, 0)
	ok(t, s.SetPermits(ctx(t), 15))
	counts(15, 15, 0)
	ok(t, s.SetPermits(ctx(t), 15))
	counts(15, 15, 0)
	ok(t, s.SetPermits(ctx(t), 5))
	counts(5, 5, 0)
	for range 3 {
		if tryAcquireWaitLease(t, s, 200*ms, time.Second) == "" {
			t.Fatal("acquire failed")
		}
	}
	counts(5, 2, 3)
	ok(t, s.SetPermits(ctx(t), 3))
	counts(3, 0, 3)
	ok(t, s.SetPermits(ctx(t), 2))
	counts(2, -1, 3)
}

func TestRedissonSemaphoreAddPermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	ok(t, s.AddPermits(ctx(t), 5))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 15, "available")
	ok(t, s.AddPermits(ctx(t), -10))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 5, "available")
	ok(t, s.AddPermits(ctx(t), -10))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), -5, "available")
}

func TestRedissonSemaphoreBlockingAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	id := must(t, of(s.Acquire(ctx(t), nanolimit.NoExpiry)))
	eq(t, len(id), 32, "permit id length")
	go func() {
		time.Sleep(time.Second)
		if err := e.client().Semaphore(e.name("test")).Release(ctx(t), id); err != nil {
			t.Errorf("Release: %v", err)
		}
	}()
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 0, "available")
	must(t, of(s.Acquire(ctx(t), nanolimit.NoExpiry)))
	_, acquired, err := s.TryAcquire(ctx(t), nanolimit.NoExpiry)
	ok(t, err)
	eq(t, acquired, false, "TryAcquire")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 0, "available")
}

func TestRedissonSemaphoreBlockingAcquireMany(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	ids := must(t, of(s.AcquireN(ctx(t), 6, nanolimit.NoExpiry)))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
	ok(t, s.ReleaseN(ctx(t), ids))
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 10, "available")
}

func TestRedissonSemaphoreTryAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	id, acquired, err := s.TryAcquire(ctx(t), nanolimit.NoExpiry)
	ok(t, err)
	eq(t, acquired, true, "TryAcquire")
	eq(t, len(id), 32, "permit id length")
	_, acquired, err = s.TryAcquire(ctx(t), nanolimit.NoExpiry)
	ok(t, err)
	eq(t, acquired, false, "second TryAcquire")
	go func() {
		time.Sleep(500 * ms)
		if releaseErr := e.client().Semaphore(e.name("test")).Release(ctx(t), id); releaseErr != nil {
			t.Errorf("Release: %v", releaseErr)
		}
	}()
	start := time.Now()
	id2 := tryAcquireWaitLease(t, s, time.Second, nanolimit.NoExpiry)
	eq(t, len(id2), 32, "permit id length")
	between(t, time.Since(start), 450*ms, 600*ms, "wait for the release")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 0, "available")
}

func TestRedissonSemaphoreTryAcquireMany(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	eq(t, len(must(t, of(s.TryAcquireN(ctx(t), 4, nanolimit.NoExpiry)))), 4, "first")
	eq(t, len(must(t, of(s.TryAcquireN(ctx(t), 5, nanolimit.NoExpiry)))), 5, "second")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 1, "available")
}

// counterUnderPermit is Redisson's read-then-write counter: a lost update
// shows two holders of the one permit. The load and the store are atomic
// apart only, so the race detector stays quiet and the update can still be
// lost.
func counterUnderPermit(t *testing.T, s nanolimit.Semaphore, counter *atomic.Int64, hold time.Duration) {
	t.Helper()
	id, err := s.Acquire(ctx(t), nanolimit.NoExpiry)
	if err != nil {
		t.Errorf("Acquire: %v", err)
		return
	}
	value := counter.Load()
	time.Sleep(hold)
	counter.Store(value + 1)
	if err = s.Release(ctx(t), id); err != nil {
		t.Errorf("Release: %v", err)
	}
}

func TestRedissonSemaphoreConcurrencySingleInstance(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	must(t, of(e.sem("test").TrySetPermits(ctx(t), 1)))
	const iterations = 100
	var counter atomic.Int64
	var wg sync.WaitGroup
	for range iterations {
		wg.Go(func() { counterUnderPermit(t, e.sem("test"), &counter, 0) })
	}
	wg.Wait()
	eq(t, counter.Load(), iterations, "counter")
}

// Redisson: 16 clients x 100 iterations (16s). Shortened to 20 iterations.
func TestRedissonSemaphoreConcurrencyLoopMultiInstance(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	must(t, of(e.sem("test").TrySetPermits(ctx(t), 1)))
	const clients, iterations = 16, 20
	var counter atomic.Int64
	var wg sync.WaitGroup
	for range clients {
		s := e.client().Semaphore(e.name("test"))
		wg.Go(func() {
			for range iterations {
				counterUnderPermit(t, s, &counter, 10*ms)
			}
		})
	}
	wg.Wait()
	eq(t, counter.Load(), clients*iterations, "counter")
}

func TestRedissonSemaphoreAcquire(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test.sync_semaphore")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	start := time.Now()
	if id := must(t, of(s.Acquire(ctx(t), 5000*ms))); id == "" {
		t.Fatal("Acquire returned no permit")
	}
	if elapsed := time.Since(start); elapsed > 100*ms {
		t.Fatalf("Acquire took %v, want < 100ms", elapsed)
	}
}

func TestRedissonSemaphoreAcquireAsyncMany(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("test")
	must(t, of(s.TrySetPermits(ctx(t), 10)))
	start := time.Now()
	ids := must(t, of(s.AcquireN(ctx(t), 6, nanolimit.NoExpiry)))
	if elapsed := time.Since(start); elapsed > 100*ms {
		t.Fatalf("AcquireN took %v, want < 100ms", elapsed)
	}
	eq(t, len(ids), 6, "acquired")
	eq(t, must(t, of(s.AvailablePermits(ctx(t)))), 4, "available")
}

// multiPermits is testConcurrencyMultiPermits(2): two clients want n1 and
// n2 of 2 permits, hold them 500ms, and both get them within 2s.
func multiPermits(t *testing.T, n1, n2 int, stagger time.Duration) {
	t.Helper()
	e := newEnv(t)
	s := e.sem("testConcurrencyMultiPermits")
	must(t, of(s.TrySetPermits(ctx(t), 2)))
	var success atomic.Int32
	var wg sync.WaitGroup
	run := func(n int) {
		ids, err := s.TryAcquireNWait(ctx(t), n, 2*time.Second, nanolimit.NoExpiry)
		if err != nil {
			t.Errorf("TryAcquireNWait: %v", err)
			return
		}
		time.Sleep(500 * ms)
		if len(ids) > 0 {
			success.Add(1)
			if _, err = s.TryReleaseN(ctx(t), ids); err != nil {
				t.Errorf("TryReleaseN: %v", err)
			}
		}
	}
	wg.Go(func() { run(n1) })
	time.Sleep(stagger)
	wg.Go(func() { run(n2) })
	wg.Wait()
	eq(t, success.Load(), 2, "clients that got their permits")
}

func TestRedissonSemaphoreConcurrencyMultiPermits(t *testing.T) {
	t.Parallel()
	multiPermits(t, 2, 2, 0)
}

func TestRedissonSemaphoreConcurrencyMultiPermits2(t *testing.T) {
	t.Parallel()
	multiPermits(t, 1, 2, 100*ms)
}
