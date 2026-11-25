//go:build integration

package nanolimit_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// Many processes share one overall budget: across 5 clients and 40
// goroutines, one window admits exactly Rate.
func TestRateLimiterConcurrentClientsShareTheBudget(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const rate = 25
	name := e.name("shared")
	must(t, of(e.limits.RateLimiter(name).TrySetRate(ctx(t), overall(rate, time.Minute))))
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 5 {
		limiter := e.client().RateLimiter(name)
		for range 8 {
			wg.Go(func() {
				for range 10 {
					got, err := limiter.TryAcquire(ctx(t), 1)
					if err != nil {
						t.Errorf("TryAcquire: %v", err)
						return
					}
					if got {
						admitted.Add(1)
					}
				}
			})
		}
	}
	wg.Wait()
	eq(t, admitted.Load(), rate, "admitted in one window")
}

// PerClient gives each Limits its own budget under one name.
func TestRateLimiterPerClientBudgets(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	name := e.name("per-client")
	a, b := e.client(nanolimit.WithClientID("a")).
		RateLimiter(name),
		e.client(nanolimit.WithClientID("b")).
			RateLimiter(name)
	must(t, of(a.TrySetRate(ctx(t), perClient(2, time.Minute))))
	for _, limiter := range []nanolimit.RateLimiter{a, b} {
		eq(t, must(t, of(limiter.TryAcquire(ctx(t), 2))), true, "own budget")
		eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), false, "own budget spent")
	}
}

// A limiter per user must not stay in Redis: with KeepAlive every key has a
// TTL after an acquire, the keys vanish once it passes, and the next acquire
// reports ErrNotInitialized so the caller sets the rate again.
func TestRateLimiterKeepAliveCleansEveryKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("user")
	cfg := overall(5, 500*ms)
	cfg.KeepAlive = 800 * ms
	must(t, of(limiter.TrySetRate(ctx(t), cfg)))
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "TryAcquire")
	keys := must(t, of(e.rdb.Keys(ctx(t), "*"+e.ns+"*").Result()))
	eq(t, len(keys), 3, "keys: config, value, permits")
	for _, key := range keys {
		ttl := must(t, of(e.rdb.PTTL(ctx(t), key).Result()))
		between(t, ttl, 1*ms, 800*ms, "TTL of "+key)
	}
	time.Sleep(900 * ms)
	eq(t, e.keyCount(), 0, "keys after the keep-alive")
	if _, err := limiter.TryAcquire(ctx(t), 1); !errors.Is(err, nanolimit.ErrNotInitialized) {
		t.Fatalf("TryAcquire after the keep-alive = %v, want ErrNotInitialized", err)
	}
	cfgGot, err := limiter.Config(ctx(t))
	if !errors.Is(err, nanolimit.ErrNotInitialized) {
		t.Fatalf("Config = %+v, %v; want ErrNotInitialized", cfgGot, err)
	}
	must(t, of(limiter.TrySetRate(ctx(t), cfg)))
	eq(t, must(t, of(limiter.TryAcquire(ctx(t), 1))), true, "TryAcquire after setting the rate again")
	eq(t, must(t, of(limiter.Config(ctx(t)))).KeepAlive, 800*ms, "KeepAlive")
}

// An acquire gives the permit set the TTL of the semaphore, so Expire on a
// per-user semaphore cleans both keys.
func TestSemaphoreExpireCoversThePermits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.sem("user")
	must(t, of(s.TrySetPermits(ctx(t), 2)))
	eq(t, must(t, of(s.Expire(ctx(t), 700*ms))), true, "Expire")
	must(t, of(s.TryAcquireN(ctx(t), 1, time.Minute)))
	for _, key := range []string{e.name("user"), "{" + e.name("user") + "}:timeout"} {
		between(t, must(t, of(e.rdb.PTTL(ctx(t), key).Result())), 1*ms, 700*ms, "TTL of "+key)
	}
	time.Sleep(800 * ms)
	eq(t, e.keyCount(), 0, "keys")
	eq(t, must(t, of(s.Delete(ctx(t)))), false, "Delete after expiry")
}

// A crashed holder never releases: the lease brings its permits back, and
// no more than the permits are ever held at once across clients.
func TestSemaphoreLeaseReclaimAndExclusion(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const permits = 3
	name := e.name("slots")
	must(t, of(e.limits.Semaphore(name).TrySetPermits(ctx(t), permits)))

	// The crashed holders.
	crashed := must(t, of(e.client().Semaphore(name).TryAcquireN(ctx(t), permits, 300*ms)))
	eq(t, len(crashed), permits, "crashed holders")
	_, got, err := e.limits.Semaphore(name).TryAcquire(ctx(t), time.Second)
	ok(t, err)
	eq(t, got, false, "TryAcquire while every permit is held")

	var holders, maxHolders, done atomic.Int64
	var wg sync.WaitGroup
	for c := range 4 {
		s := e.client().Semaphore(name)
		for range 3 {
			wg.Go(func() {
				id, acquireErr := s.Acquire(ctx(t), 5*time.Second)
				if acquireErr != nil {
					t.Errorf("client %d: Acquire: %v", c, acquireErr)
					return
				}
				now := holders.Add(1)
				for {
					old := maxHolders.Load()
					if now <= old || maxHolders.CompareAndSwap(old, now) {
						break
					}
				}
				time.Sleep(20 * ms)
				holders.Add(-1)
				if releaseErr := s.Release(ctx(t), id); releaseErr != nil {
					t.Errorf("client %d: Release: %v", c, releaseErr)
				}
				done.Add(1)
			})
		}
	}
	wg.Wait()
	eq(t, done.Load(), 12, "holders that got a permit after the lease ended")
	if maxHolders.Load() > permits {
		t.Fatalf("%d holders at once, permits %d", maxHolders.Load(), permits)
	}
	eq(t, must(t, of(e.limits.Semaphore(name).AvailablePermits(ctx(t)))), permits, "available")
	eq(t, must(t, of(e.limits.Semaphore(name).AcquiredPermits(ctx(t)))), 0, "acquired")
	for _, id := range crashed {
		eq(t, must(t, of(e.limits.Semaphore(name).TryRelease(ctx(t), id))), false, "late release of a reclaimed permit")
	}
}

// A wait ends with its context: the error is the context's, and FailOpen
// does not admit a caller whose context ended.
func TestWaitsEndWithTheContext(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limiter := e.rate("deadline")
	must(t, of(limiter.TrySetRate(ctx(t), overall(1, time.Minute))))
	must(t, of(limiter.TryAcquire(ctx(t), 1)))
	s := e.sem("deadline-semaphore")
	must(t, of(s.TrySetPermits(ctx(t), 1)))
	must(t, of(s.TryAcquireN(ctx(t), 1, nanolimit.NoExpiry)))

	for what, wait := range map[string]func(context.Context) error{
		"RateLimiter.Acquire": func(c context.Context) error { return limiter.Acquire(c, 1) },
		"Semaphore.Acquire":   func(c context.Context) error { _, err := s.Acquire(c, nanolimit.NoExpiry); return err },
	} {
		c, cancel := context.WithTimeout(ctx(t), 200*ms)
		start := time.Now()
		err := wait(c)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s = %v, want context.DeadlineExceeded", what, err)
		}
		between(t, time.Since(start), 150*ms, 600*ms, what+" duration")
	}

	// TryAcquireWait returns false at its own timeout, before the context's.
	start := time.Now()
	eq(t, must(t, of(limiter.TryAcquireWait(ctx(t), 1, 150*ms))), false, "TryAcquireWait")
	between(t, time.Since(start), 100*ms, 500*ms, "TryAcquireWait duration")
	start = time.Now()
	_, got, err := s.TryAcquireWait(ctx(t), 150*ms, nanolimit.NoExpiry)
	ok(t, err)
	eq(t, got, false, "Semaphore.TryAcquireWait")
	between(t, time.Since(start), 100*ms, 500*ms, "Semaphore.TryAcquireWait duration")
}

// The module's constructor uses the client of the nanocache store.
func TestNewFromStoreUsesTheStoreClient(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	limits := nanolimit.NewFromStore(nanocache.NewRedisStore(e.rdb), discard())
	limiter := limits.RateLimiter(e.name("store"))
	eq(t, must(t, of(limiter.TrySetRate(ctx(t), overall(1, time.Minute)))), true, "TrySetRate")
	eq(t, e.keyCount(), 1, "keys")
}
