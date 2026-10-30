//go:build integration

package redisx_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

// TestMutexLockWakesAtOnceOnRelease measures latency, so it runs before the parallel tests.
func TestMutexLockWakesAtOnceOnRelease(t *testing.T) {
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("wake")

	for range 3 {
		held, err := mutex.TryLock(ctx)
		require.NoError(t, err)
		acquired := make(chan *redisx.Lease, 1)
		go func() {
			lease, err := mutex.Lock(ctx)
			assert.NoError(t, err)
			acquired <- lease
		}()
		// Long enough for a polling waiter to reach its longest pause.
		time.Sleep(1500 * time.Millisecond)
		released := time.Now()
		require.NoError(t, held.Release(ctx))
		lease := <-acquired
		require.Less(t, time.Since(released), 100*time.Millisecond)
		require.NoError(t, lease.Release(ctx))
	}
}

// TestMutexLockTakesACrashedHoldersLeaseWhenItExpires measures latency, so it runs before the parallel tests.
func TestMutexLockTakesACrashedHoldersLeaseWhenItExpires(t *testing.T) {
	ctx := t.Context()
	const ttl = 500 * time.Millisecond
	mutex := redistest.New(t).Mutex("expired").WithTTL(ttl)

	for range 5 {
		start := time.Now()
		_, err := mutex.TryLock(ctx) // never refreshed nor released
		require.NoError(t, err)
		lease, err := mutex.Lock(ctx)
		require.NoError(t, err)
		require.Less(t, time.Since(start), ttl+75*time.Millisecond)
		require.NoError(t, lease.Release(ctx))
	}
}

func TestMutexDoKeepsTheLeaseUntilTheWorkReturns(t *testing.T) {
	t.Parallel()
	mutex := redistest.New(t).Mutex("stopping").WithTTL(300 * time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	err := mutex.Do(ctx, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		// The work takes a while to stop and must stay alone meanwhile.
		for range 10 {
			time.Sleep(100 * time.Millisecond)
			if _, err := mutex.TryLock(t.Context()); !errors.Is(err, redisx.ErrLocked) {
				return errors.New("another holder got the mutex while the work stopped")
			}
		}
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, redisx.ErrLeaseLost)
}

func TestMutexDoCancelsTheWorkWhenAPausedServerLetsTheLeaseExpire(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	const ttl = time.Second
	mutex := server.New(t).Mutex("paused").WithTTL(ttl)

	var waited time.Duration
	var cause error
	err := mutex.Do(t.Context(), func(ctx context.Context) error {
		server.Pause(t)
		paused := time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		waited = time.Since(paused)
		cause = context.Cause(ctx)
		server.Resume(t)
		return nil
	})
	require.ErrorIs(t, err, redisx.ErrLeaseLost)
	require.ErrorIs(t, cause, redisx.ErrLeaseLost)
	// The last refresh that succeeded was sent before the pause, so the
	// lease expired at most one TTL after it.
	require.Less(t, waited, ttl+300*time.Millisecond)
}

func TestMutexAfterARestartThatLostTheData(t *testing.T) {
	t.Parallel()
	server := redistest.StartServer(t)
	rx := server.New(t)
	mutex := rx.Mutex("restarted").WithTTL(600 * time.Millisecond)

	var before int64
	for range 3 {
		lease, err := mutex.TryLock(t.Context())
		require.NoError(t, err)
		before = lease.Fence()
		require.NoError(t, lease.Release(t.Context()))
	}

	err := mutex.Do(t.Context(), func(ctx context.Context) error {
		server.Restart(t)
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return nil
	})
	require.ErrorIs(t, err, redisx.ErrLeaseLost, "the restart dropped the lease")

	// The server lost its scripts and its fence counter.
	lease, err := mutex.TryLock(t.Context())
	require.NoError(t, err)
	require.Greater(t, lease.Fence(), before)
	require.NoError(t, lease.Release(t.Context()))
}

func TestMutexTryLockHoldsTheMutexWhenTheReplyIsLost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rdb, faults := redistest.FaultyClient(t, redistest.Addr(t))
	mutex := redisx.New(rdb, redisx.WithPrefix(redistest.Namespace(t))).Mutex("retried")
	warm, err := mutex.TryLock(ctx) // loads the scripts
	require.NoError(t, err)
	require.NoError(t, warm.Release(ctx))

	// go-redis sends the script again and finds the lease it took itself.
	faults.LoseReply("evalsha")
	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, faults.Lost())
	require.Greater(t, lease.Fence(), warm.Fence())

	_, err = mutex.TryLock(ctx)
	require.ErrorIs(t, err, redisx.ErrLocked)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexTryLockGivesTheLeaseBackWhenTheReplyIsLost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rdb, faults := redistest.FaultyClient(t, redistest.Addr(t), func(o *redis.Options) { o.MaxRetries = -1 })
	mutex := redisx.New(rdb, redisx.WithPrefix(redistest.Namespace(t))).Mutex("orphan")
	warm, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, warm.Release(ctx))

	faults.LoseReply("evalsha")
	_, err = mutex.TryLock(ctx)
	require.Error(t, err)
	require.NotErrorIs(t, err, redisx.ErrLocked)
	require.Equal(t, 1, faults.Lost())

	// The server took the lease, but TryLock gave it back at once.
	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexReleaseSucceedsWhenTheReplyIsLost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rdb, faults := redistest.FaultyClient(t, redistest.Addr(t))
	mutex := redisx.New(rdb, redisx.WithPrefix(redistest.Namespace(t))).Mutex("released")

	err := mutex.Do(ctx, func(context.Context) error {
		// go-redis sends the release again after the first one freed the
		// mutex.
		faults.LoseReply("evalsha")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, faults.Lost())

	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexKeepsManyClientsApart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	namespace := redistest.Namespace(t)
	const clients, rounds = 8, 25

	var wg sync.WaitGroup
	for range clients {
		// One connection pool each, as separate processes would have.
		rx := redisx.New(redistest.Client(t), redisx.WithPrefix(namespace))
		mutex := rx.Mutex("shared")
		count := rx.Value[int]("count")
		wg.Go(func() {
			for range rounds {
				err := mutex.Do(ctx, func(ctx context.Context) error {
					// A read, then a write: only exclusion keeps every
					// increment.
					current, err := count.Get(ctx)
					if err != nil {
						return err
					}
					return count.Set(ctx, current.OrElse(0)+1)
				})
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()

	final, err := redisx.New(redistest.Client(t), redisx.WithPrefix(namespace)).Value[int]("count").Get(ctx)
	require.NoError(t, err)
	require.Equal(t, clients*rounds, final.OrElse(0))
}
