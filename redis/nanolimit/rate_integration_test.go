//go:build integration

package nanolimit_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// The rate limiter against the shared server of redis/internal/redistest:
//
//	go test -tags=integration ./redis/nanolimit/

func TestRateAdmitsTheBurstThenRefusesWithRetryAfter(t *testing.T) {
	t.Parallel()
	limiter := mustRate(t, redisLimits(t), redistest.Namespace(t)+":calls", nanolimit.PerMinute(60).WithBurst(3))
	for i := range 3 {
		decision := mustAllow(t, limiter, "user")
		if !decision.Allowed || decision.Remaining != 2-i {
			t.Fatalf("event %d = %+v, want admitted with %d remaining", i+1, decision, 2-i)
		}
	}
	decision := mustAllow(t, limiter, "user")
	if decision.Allowed || decision.Remaining != 0 {
		t.Fatalf("event 4 = %+v, want refused with 0 remaining", decision)
	}
	// One event per second: the next one is due within the second.
	if decision.RetryAfter <= 900*time.Millisecond || decision.RetryAfter > time.Second {
		t.Errorf("RetryAfter = %s, want just under 1s", decision.RetryAfter)
	}
	if decision.ResetAfter <= 2900*time.Millisecond || decision.ResetAfter > 3*time.Second {
		t.Errorf("ResetAfter = %s, want just under 3s", decision.ResetAfter)
	}
	if other := mustAllow(t, limiter, "other-user"); !other.Allowed {
		t.Errorf("another key was refused: %+v", other)
	}
}

func TestRateAdmitsAgainAfterRetryAfter(t *testing.T) {
	t.Parallel()
	limiter := mustRate(t, redisLimits(t), redistest.Namespace(t)+":calls", nanolimit.PerSecond(10).WithBurst(1))
	if first := mustAllow(t, limiter, "k"); !first.Allowed {
		t.Fatalf("first event refused: %+v", first)
	}
	refused := mustAllow(t, limiter, "k")
	if refused.Allowed {
		t.Fatal("second event admitted at once with a burst of 1")
	}
	time.Sleep(refused.RetryAfter + 5*time.Millisecond)
	if again := mustAllow(t, limiter, "k"); !again.Allowed {
		t.Errorf("event after RetryAfter %s refused: %+v", refused.RetryAfter, again)
	}
}

func TestAllowNTakesAllOrNothing(t *testing.T) {
	t.Parallel()
	limiter := mustRate(t, redisLimits(t), redistest.Namespace(t)+":calls", nanolimit.PerHour(5))
	decision, err := limiter.AllowN(t.Context(), "k", 3)
	if err != nil || !decision.Allowed || decision.Remaining != 2 {
		t.Fatalf("AllowN 3 of 5 = %+v, %v; want admitted with 2 remaining", decision, err)
	}
	decision, err = limiter.AllowN(t.Context(), "k", 3)
	if err != nil || decision.Allowed || decision.Remaining != 2 {
		t.Fatalf("AllowN 3 with 2 left = %+v, %v; want refused with 2 still remaining", decision, err)
	}
	if decision, err = limiter.AllowN(t.Context(), "k", 2); err != nil || !decision.Allowed || decision.Remaining != 0 {
		t.Errorf("AllowN 2 with 2 left = %+v, %v; want admitted with 0 remaining", decision, err)
	}
}

// Over two seconds, a key that sends as fast as it can gets its burst plus
// one event per spacing, not a new burst per period.
func TestRateHoldsOverTime(t *testing.T) {
	t.Parallel()
	limiter := mustRate(t, redisLimits(t), redistest.Namespace(t)+":calls", nanolimit.PerSecond(20).WithBurst(5))
	var admitted int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mustAllow(t, limiter, "k").Allowed {
			admitted++
		}
		time.Sleep(2 * time.Millisecond)
	}
	// 5 at once, then 20 per second for 2 seconds: 45.
	if admitted < 42 || admitted > 46 {
		t.Errorf("admitted %d events in 2s at 20/s with a burst of 5, want about 45", admitted)
	}
}

// Replicas are separate clients: together they admit exactly the burst.
func TestRateIsSharedByConcurrentClients(t *testing.T) {
	t.Parallel()
	name := redistest.Namespace(t) + ":calls"
	const replicas, perReplica, burst = 8, 25, 50
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range replicas {
		limiter := mustRate(t, redisLimits(t), name, nanolimit.PerHour(burst))
		wg.Go(func() {
			for range perReplica {
				decision, err := limiter.Allow(t.Context(), "shared")
				if err != nil {
					t.Error(err)
					return
				}
				if decision.Allowed {
					admitted.Add(1)
				}
			}
		})
	}
	wg.Wait()
	if got := admitted.Load(); got != burst {
		t.Errorf("%d replicas admitted %d of %d events, want exactly the burst of %d",
			replicas, got, replicas*perReplica, burst)
	}
}

// A key is stored only until it has its full burst again.
func TestRateKeyExpiresWhenIdle(t *testing.T) {
	t.Parallel()
	name := redistest.Namespace(t) + ":calls"
	limiter := mustRate(t, redisLimits(t), name, nanolimit.PerSecond(10).WithBurst(2))
	mustAllow(t, limiter, "k")
	client := rawClient(t)
	ttl, err := client.PTTL(t.Context(), name+":k").Result()
	if err != nil || ttl <= 0 || ttl > 100*time.Millisecond {
		t.Fatalf("PTTL after one event = %s, %v; want at most one spacing of 100ms", ttl, err)
	}
	time.Sleep(150 * time.Millisecond)
	if n, existsErr := client.Exists(t.Context(), name+":k").Result(); existsErr != nil || n != 0 {
		t.Errorf("key still stored after it was full again: exists %d, %v", n, existsErr)
	}
}

func redisLimits(t *testing.T) *nanolimit.Limits {
	t.Helper()
	store := nanocache.NewRedisStore(redis.NewClient(&redis.Options{Addr: redistest.Addr(t)}))
	t.Cleanup(func() { _ = store.Close() })
	return nanolimit.FromStore(store, nanolimit.WithTimeout(time.Second))
}

func rawClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mustAllow(t *testing.T, limiter nanolimit.RateLimiter, key string) nanolimit.Decision {
	t.Helper()
	decision, err := limiter.Allow(t.Context(), key)
	if err != nil {
		t.Fatalf("Allow(%q): %v", key, err)
	}
	return decision
}
