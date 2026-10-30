//go:build integration

package redisx_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/redistest"
)

func TestMutexTryLockRefusesASecondHolder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("provision:org_acme")

	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)

	_, err = mutex.TryLock(ctx)
	require.ErrorIs(t, err, redisx.ErrLocked)

	require.NoError(t, lease.Release(ctx))
	again, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, again.Release(ctx))
}

func TestMutexNamesAreIndependent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)

	a, err := rx.Mutex("a").TryLock(ctx)
	require.NoError(t, err)
	b, err := rx.Mutex("b").TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, a.Release(ctx))
	require.NoError(t, b.Release(ctx))
}

func TestMutexPrefixesSeparateClients(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	first, err := redistest.New(t).Mutex("same").TryLock(ctx)
	require.NoError(t, err)
	second, err := redistest.New(t).Mutex("same").TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, first.Release(ctx))
	require.NoError(t, second.Release(ctx))
}

func TestMutexFenceGrowsWithEveryLease(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("fenced")

	var previous int64
	for range 5 {
		lease, err := mutex.TryLock(ctx)
		require.NoError(t, err)
		require.Greater(t, lease.Fence(), previous)
		previous = lease.Fence()
		require.NoError(t, lease.Release(ctx))
	}
}

func TestMutexKeysShareAHashTag(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)

	lease, err := rx.Mutex("tagged").TryLock(ctx)
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()

	tag := "{" + rx.Key("tagged") + "}"
	exists, err := rx.Redis().Exists(ctx, tag+":lease", tag+":fence").Result()
	require.NoError(t, err)
	require.Equal(t, int64(2), exists)
}

func TestMutexLeaseExpiresWithoutRefresh(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("crashed").WithTTL(150 * time.Millisecond)

	stale, err := mutex.TryLock(ctx)
	require.NoError(t, err)

	var fresh *redisx.Lease
	require.Eventually(t, func() bool {
		fresh, err = mutex.TryLock(ctx)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)
	require.Greater(t, fresh.Fence(), stale.Fence())

	// The old holder can no longer touch the new holder's lease.
	require.ErrorIs(t, stale.Release(ctx), redisx.ErrLeaseLost)
	require.ErrorIs(t, stale.Refresh(ctx), redisx.ErrLeaseLost)
	_, err = stale.TTL(ctx)
	require.ErrorIs(t, err, redisx.ErrLeaseLost)

	_, err = mutex.TryLock(ctx)
	require.ErrorIs(t, err, redisx.ErrLocked)
	require.NoError(t, fresh.Release(ctx))
}

func TestMutexRefreshExtendsTheLease(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("refreshed").WithTTL(300 * time.Millisecond)

	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	for range 4 {
		time.Sleep(150 * time.Millisecond)
		require.NoError(t, lease.Refresh(ctx))
	}
	ttl, err := lease.TTL(ctx)
	require.NoError(t, err)
	require.Greater(t, ttl, 200*time.Millisecond)
	require.LessOrEqual(t, ttl, 300*time.Millisecond)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexReleaseTwiceReportsTheLeaseLost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	lease, err := redistest.New(t).Mutex("twice").TryLock(ctx)
	require.NoError(t, err)

	require.NoError(t, lease.Release(ctx))
	require.ErrorIs(t, lease.Release(ctx), redisx.ErrLeaseLost)
}

func TestMutexLockWaitsForTheRelease(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("queue")

	first, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	go func() {
		time.Sleep(200 * time.Millisecond)
		assert.NoError(t, first.Release(ctx))
	}()

	start := time.Now()
	second, err := mutex.Lock(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
	require.Greater(t, second.Fence(), first.Fence())
	require.NoError(t, second.Release(ctx))
}

func TestMutexLockStopsAtTheWaitCap(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("capped")

	held, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	defer func() { _ = held.Release(ctx) }()

	start := time.Now()
	_, err = mutex.WithWait(200 * time.Millisecond).Lock(ctx)
	require.ErrorIs(t, err, redisx.ErrLocked)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestMutexLockReturnsTheContextCause(t *testing.T) {
	t.Parallel()
	mutex := redistest.New(t).Mutex("cancelled")

	held, err := mutex.TryLock(t.Context())
	require.NoError(t, err)
	defer func() { _ = held.Release(t.Context()) }()

	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	_, err = mutex.Lock(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, redisx.ErrLocked)
}

func TestMutexDoRunsOneHolderAtATime(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("exclusive")

	var inside, maxInside, runs atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			err := mutex.Do(ctx, func(context.Context) error {
				now := inside.Add(1)
				for {
					seen := maxInside.Load()
					if now <= seen || maxInside.CompareAndSwap(seen, now) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				inside.Add(-1)
				runs.Add(1)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), maxInside.Load())
	require.Equal(t, int32(12), runs.Load())
}

func TestMutexDoKeepsTheLeaseAlivePastItsTTL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("long").WithTTL(300 * time.Millisecond)

	err := mutex.Do(ctx, func(ctx context.Context) error {
		for range 6 {
			time.Sleep(150 * time.Millisecond)
			if _, err := mutex.TryLock(ctx); !errors.Is(err, redisx.ErrLocked) {
				return errors.New("another holder got the mutex")
			}
		}
		return ctx.Err()
	})
	require.NoError(t, err)

	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err, "Do released the mutex")
	require.NoError(t, lease.Release(ctx))
}

func TestMutexDoCancelsTheWorkWhenTheLeaseIsLost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	mutex := rx.Mutex("stolen").WithTTL(300 * time.Millisecond)

	var cause error
	err := mutex.Do(ctx, func(ctx context.Context) error {
		// Someone deletes the lease behind the holder's back.
		tag := "{" + rx.Key("stolen") + "}"
		if err := rx.Redis().Del(ctx, tag+":lease").Err(); err != nil {
			return err
		}
		<-ctx.Done()
		cause = context.Cause(ctx)
		return ctx.Err()
	})
	require.ErrorIs(t, err, redisx.ErrLeaseLost)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, cause, redisx.ErrLeaseLost)
}

func TestMutexDoReportsALeaseThatExpiredBeforeRelease(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rx := redistest.New(t)
	mutex := rx.Mutex("late").WithTTL(time.Minute)

	err := mutex.Do(ctx, func(ctx context.Context) error {
		tag := "{" + rx.Key("late") + "}"
		return rx.Redis().Del(ctx, tag+":lease").Err()
	})
	require.ErrorIs(t, err, redisx.ErrLeaseLost)
}

func TestMutexDoReturnsTheWorkErrorAndReleases(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("failing")
	failed := errors.New("failed")

	err := mutex.Do(ctx, func(context.Context) error { return failed })
	require.ErrorIs(t, err, failed)
	require.NotErrorIs(t, err, redisx.ErrLeaseLost)

	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexDoReleasesWhenTheWorkPanics(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("panicking")

	require.Panics(t, func() {
		_ = mutex.Do(ctx, func(context.Context) error { panic("boom") })
	})
	lease, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexDoReleasesWhenTheCallerContextEnds(t *testing.T) {
	t.Parallel()
	mutex := redistest.New(t).Mutex("abandoned")

	ctx, cancel := context.WithCancel(t.Context())
	err := mutex.Do(ctx, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, redisx.ErrLeaseLost)

	lease, err := mutex.TryLock(t.Context())
	require.NoError(t, err)
	require.NoError(t, lease.Release(t.Context()))
}

func TestMutexTryDoRefusesWhileHeld(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mutex := redistest.New(t).Mutex("busy")

	held, err := mutex.TryLock(ctx)
	require.NoError(t, err)
	ran := false
	err = mutex.TryDo(ctx, func(context.Context) error { ran = true; return nil })
	require.ErrorIs(t, err, redisx.ErrLocked)
	require.False(t, ran)

	require.NoError(t, held.Release(ctx))
	require.NoError(t, mutex.TryDo(ctx, func(context.Context) error { ran = true; return nil }))
	require.True(t, ran)
}

func TestMutexWithTTLIgnoresNonPositiveValues(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	lease, err := redistest.New(t).Mutex("default").WithTTL(0).TryLock(ctx)
	require.NoError(t, err)
	ttl, err := lease.TTL(ctx)
	require.NoError(t, err)
	require.Greater(t, ttl, redisx.DefaultLeaseTTL-5*time.Second)
	require.NoError(t, lease.Release(ctx))
}

func TestMutexDoGivesUpWhenTheServerStopsAnsweringForATTL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	rdb := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	mutex := redisx.New(rdb, redisx.WithPrefix(redistest.Namespace(t))).Mutex("unreachable").
		WithTTL(300 * time.Millisecond)

	start := time.Now()
	err := mutex.Do(ctx, func(ctx context.Context) error {
		// Every refresh now fails without telling whether the lease is lost.
		if err := rdb.Close(); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
	require.ErrorIs(t, err, redisx.ErrLeaseLost)
	require.ErrorContains(t, err, "no refresh succeeded")
	require.Less(t, time.Since(start), 2*time.Second)
}
