//go:build integration

package semaphore_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/semaphore"
)

func TestSemaphoreLeasesUpToItsPermits(t *testing.T) {
	t.Parallel()
	slots := redisPool(t, redistest.Namespace(t)+":slots", 2)
	first := mustAcquire(t, slots, "user", time.Minute)
	second := mustAcquire(t, slots, "user", time.Minute)
	if !first.Acquired || first.Available != 1 || !second.Acquired || second.Available != 0 {
		t.Fatalf("first two = %+v, %+v; want both acquired, leaving 1 then 0", first, second)
	}
	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("permit IDs %q and %q, want two distinct IDs", first.ID, second.ID)
	}
	third := mustAcquire(t, slots, "user", time.Minute)
	if third.Acquired || third.ID != "" {
		t.Fatalf("third = %+v, want refused without ID", third)
	}
	if third.RetryAfter <= 59*time.Second || third.RetryAfter > time.Minute {
		t.Errorf("RetryAfter = %s, want the earliest lease, just under 1m", third.RetryAfter)
	}
	if other := mustAcquire(t, slots, "other-user", time.Minute); !other.Acquired {
		t.Errorf("another key was refused: %+v", other)
	}
	if err := slots.Release(t.Context(), "user", first.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if n, err := slots.Available(t.Context(), "user"); err != nil || n != 1 {
		t.Errorf("Available after a release = %d, %v; want 1", n, err)
	}
	if again := mustAcquire(t, slots, "user", time.Minute); !again.Acquired {
		t.Errorf("Acquire after a release = %+v, want acquired", again)
	}
}

// A holder that never releases (a crash) frees its permit when the lease
// ends, and its late release must not free the permit of the next holder.
func TestSemaphoreReclaimsAnEndedLease(t *testing.T) {
	t.Parallel()
	slots := redisPool(t, redistest.Namespace(t)+":slots", 1)
	crashed := mustAcquire(t, slots, "user", 100*time.Millisecond)
	if blocked := mustAcquire(t, slots, "user", time.Minute); blocked.Acquired {
		t.Fatal("a second permit was leased while the only one is held")
	}
	time.Sleep(150 * time.Millisecond)
	next := mustAcquire(t, slots, "user", time.Minute)
	if !next.Acquired {
		t.Fatalf("the permit of an ended lease was not reclaimed: %+v", next)
	}
	if err := slots.Release(t.Context(), "user", crashed.ID); !errors.Is(err, semaphore.ErrPermitNotHeld) {
		t.Fatalf("late Release = %v, want ErrPermitNotHeld", err)
	}
	if n, err := slots.Available(t.Context(), "user"); err != nil || n != 0 {
		t.Errorf("Available after a late release = %d, %v; want 0: the next holder keeps its permit", n, err)
	}
}

func TestSemaphoreDoubleReleaseIsRefused(t *testing.T) {
	t.Parallel()
	slots := redisPool(t, redistest.Namespace(t)+":slots", 2)
	permit := mustAcquire(t, slots, "user", time.Minute)
	held := mustAcquire(t, slots, "user", time.Minute)
	if err := slots.Release(t.Context(), "user", permit.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := slots.Release(t.Context(), "user", permit.ID); !errors.Is(err, semaphore.ErrPermitNotHeld) {
		t.Fatalf("second Release = %v, want ErrPermitNotHeld", err)
	}
	if n, err := slots.Available(t.Context(), "user"); err != nil || n != 1 {
		t.Errorf("Available = %d, %v; want 1: the double release must not free %s", n, err, held.ID)
	}
}

func TestSemaphoreExtendKeepsThePermit(t *testing.T) {
	t.Parallel()
	slots := redisPool(t, redistest.Namespace(t)+":slots", 1)
	permit := mustAcquire(t, slots, "user", 150*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if err := slots.Extend(t.Context(), "user", permit.ID, time.Minute); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // past the first lease
	if other := mustAcquire(t, slots, "user", time.Minute); other.Acquired {
		t.Fatal("the extended permit was reclaimed at the end of its first lease")
	}
	if err := slots.Release(t.Context(), "user", permit.ID); err != nil {
		t.Fatalf("Release after Extend: %v", err)
	}
	if err := slots.Extend(t.Context(), "user", permit.ID, time.Minute); !errors.Is(err, semaphore.ErrPermitNotHeld) {
		t.Errorf("Extend of a released permit = %v, want ErrPermitNotHeld", err)
	}
}

// Replicas are separate clients: together they never hold more than the
// permits.
func TestSemaphoreIsSharedByConcurrentClients(t *testing.T) {
	t.Parallel()
	name := redistest.Namespace(t) + ":slots"
	const replicas, rounds, permits = 8, 20, 3
	var holding, most atomic.Int64
	var wg sync.WaitGroup
	for range replicas {
		slots := redisPool(t, name, permits)
		wg.Go(func() {
			for range rounds {
				permit, err := slots.Acquire(t.Context(), "shared", time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if !permit.Acquired {
					continue
				}
				now := holding.Add(1)
				for {
					seen := most.Load()
					if now <= seen || most.CompareAndSwap(seen, now) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				holding.Add(-1)
				if err = slots.Release(t.Context(), "shared", permit.ID); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if got := most.Load(); got < 1 || got > permits {
		t.Errorf("%d replicas held up to %d permits at once, want 1 to %d", replicas, got, permits)
	}
	if n, err := newSlots(t, name, permits).Available(t.Context(), "shared"); err != nil || n != permits {
		t.Errorf("Available after every release = %d, %v; want %d", n, err, permits)
	}
}

// The key lives as long as its longest lease and goes with its last permit.
func TestSemaphoreKeyExpiresWithItsLeases(t *testing.T) {
	t.Parallel()
	name := redistest.Namespace(t) + ":slots"
	slots := redisPool(t, name, 2)
	client := rawClient(t)
	short := mustAcquire(t, slots, "user", 100*time.Millisecond)
	ttl, err := client.PTTL(t.Context(), name+":user").Result()
	if err != nil || ttl <= 0 || ttl > 100*time.Millisecond {
		t.Fatalf("PTTL with one 100ms lease = %s, %v; want at most 100ms", ttl, err)
	}
	time.Sleep(150 * time.Millisecond)
	if n, _ := client.Exists(t.Context(), name+":user").Result(); n != 0 {
		t.Fatalf("key still stored after its only lease ended (%s)", short.ID)
	}
	long := mustAcquire(t, slots, "user", time.Minute)
	if err = slots.Release(t.Context(), "user", long.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.Exists(t.Context(), name+":user").Result(); n != 0 {
		t.Error("key still stored after its last permit was released")
	}
}

func newSlots(t *testing.T, name string, permits int) semaphore.Pool {
	t.Helper()
	return redisPool(t, name, permits)
}

func redisPool(t *testing.T, name string, permits int) semaphore.Pool {
	t.Helper()
	store := cache.NewRedisStore(redis.NewClient(&redis.Options{Addr: redistest.Addr(t)}))
	t.Cleanup(func() { _ = store.Close() })
	pool, err := semaphore.New(store, name, permits, nanoredis.WithTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func rawClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mustAcquire(t *testing.T, slots semaphore.Pool, key string, lease time.Duration) semaphore.Permit {
	t.Helper()
	permit, err := slots.Acquire(t.Context(), key, lease)
	if err != nil {
		t.Fatalf("Acquire(%q): %v", key, err)
	}
	return permit
}
