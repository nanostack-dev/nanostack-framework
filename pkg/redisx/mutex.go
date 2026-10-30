package redisx

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultLeaseTTL is how long a lease lasts when the Mutex has no WithTTL.
	DefaultLeaseTTL = 30 * time.Second

	minRetryDelay = 5 * time.Millisecond
	maxRetryDelay = 250 * time.Millisecond
	releaseBudget = 5 * time.Second
)

// The lease key and the fence key share a hash tag, so the scripts also run on
// Redis Cluster.
var (
	acquireScript = redis.NewScript(`
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then
	return redis.call('INCR', KEYS[2])
end
return 0`)
	refreshScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0`)
	releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('DEL', KEYS[1])
end
return 0`)
	ttlScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PTTL', KEYS[1])
end
return -3`)
)

// Mutex is a lock shared by every process that uses the same server and name.
//
// A holder gets a Lease that expires after the TTL unless it is refreshed, so a
// crashed holder cannot block the others forever. The other side of this is
// that a holder paused for longer than the TTL loses the lease while it still
// runs. Each lease carries a fencing token that grows with every acquisition:
// pass it to the resource you protect if that resource can refuse stale
// writers.
type Mutex struct {
	rdb      redis.UniversalClient
	name     string
	leaseKey string
	fenceKey string
	ttl      time.Duration
	wait     time.Duration
}

// Mutex returns the mutex called name. Nothing is read or written until you
// call a method.
func (c *Client) Mutex(name string) Mutex {
	tag := "{" + c.Key(name) + "}"
	return Mutex{
		rdb:      c.rdb,
		name:     name,
		leaseKey: tag + ":lease",
		fenceKey: tag + ":fence",
		ttl:      DefaultLeaseTTL,
	}
}

// WithTTL sets how long a lease lasts without a refresh. Do refreshes it every
// third of ttl.
func (m Mutex) WithTTL(ttl time.Duration) Mutex {
	if ttl > 0 {
		m.ttl = ttl
	}
	return m
}

// WithWait caps how long Lock and Do wait for the mutex, independently of the
// context. When the cap is reached they return ErrLocked. Zero, the default,
// waits as long as the context allows.
func (m Mutex) WithWait(wait time.Duration) Mutex {
	m.wait = wait
	return m
}

// Lock waits until it holds the mutex and returns the lease. It returns
// ErrLocked when the WithWait cap is reached, and the context error when ctx
// ends first.
func (m Mutex) Lock(ctx context.Context) (*Lease, error) {
	var deadline <-chan time.Time
	if m.wait > 0 {
		timer := time.NewTimer(m.wait)
		defer timer.Stop()
		deadline = timer.C
	}
	delay := minRetryDelay
	for {
		lease, err := m.TryLock(ctx)
		if !errors.Is(err, ErrLocked) {
			return lease, err
		}
		pause := time.NewTimer(mrand.N(delay) + time.Millisecond)
		select {
		case <-ctx.Done():
			pause.Stop()
			return nil, fmt.Errorf("redisx: wait for mutex %s: %w", m.name, context.Cause(ctx))
		case <-deadline:
			pause.Stop()
			return nil, fmt.Errorf("redisx: wait %s for mutex %s: %w", m.wait, m.name, ErrLocked)
		case <-pause.C:
		}
		delay = min(2*delay, maxRetryDelay)
	}
}

// TryLock takes the mutex if it is free, and returns ErrLocked if it is not.
func (m Mutex) TryLock(ctx context.Context) (*Lease, error) {
	token := rand.Text()
	fence, err := acquireScript.Run(ctx, m.rdb,
		[]string{m.leaseKey, m.fenceKey}, token, m.ttl.Milliseconds()).Int64()
	if err != nil {
		return nil, fmt.Errorf("redisx: lock %s: %w", m.name, err)
	}
	if fence == 0 {
		return nil, fmt.Errorf("redisx: lock %s: %w", m.name, ErrLocked)
	}
	return &Lease{mutex: m, token: token, fence: fence}, nil
}

// Do runs fn while it holds the mutex. It waits for the mutex like Lock,
// refreshes the lease while fn runs and releases it after.
//
// If the lease is lost while fn runs, Do cancels the context it gave fn with
// ErrLeaseLost as the cause and returns an error that matches ErrLeaseLost:
// fn's work was then possibly not exclusive.
func (m Mutex) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	lease, err := m.Lock(ctx)
	if err != nil {
		return err
	}
	return lease.run(ctx, fn)
}

// TryDo is Do that returns ErrLocked at once when the mutex is held.
func (m Mutex) TryDo(ctx context.Context, fn func(ctx context.Context) error) error {
	lease, err := m.TryLock(ctx)
	if err != nil {
		return err
	}
	return lease.run(ctx, fn)
}

// Lease is one hold on a Mutex.
type Lease struct {
	mutex Mutex
	token string
	fence int64
}

// Fence returns the fencing token: it is greater than the token of every
// earlier lease on the same mutex.
func (l *Lease) Fence() int64 { return l.fence }

// Refresh extends the lease to the mutex TTL from now. It returns ErrLeaseLost
// when the lease already expired or another holder has the mutex.
func (l *Lease) Refresh(ctx context.Context) error {
	ok, err := refreshScript.Run(ctx, l.mutex.rdb,
		[]string{l.mutex.leaseKey}, l.token, l.mutex.ttl.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("redisx: refresh lease on %s: %w", l.mutex.name, err)
	}
	if ok == 0 {
		return fmt.Errorf("redisx: refresh lease on %s: %w", l.mutex.name, ErrLeaseLost)
	}
	return nil
}

// TTL returns how long the lease lasts without a refresh, or ErrLeaseLost.
func (l *Lease) TTL(ctx context.Context) (time.Duration, error) {
	ms, err := ttlScript.Run(ctx, l.mutex.rdb, []string{l.mutex.leaseKey}, l.token).Int64()
	if err != nil {
		return 0, fmt.Errorf("redisx: read lease on %s: %w", l.mutex.name, err)
	}
	if ms < 0 {
		return 0, fmt.Errorf("redisx: read lease on %s: %w", l.mutex.name, ErrLeaseLost)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// Release frees the mutex. It returns ErrLeaseLost when the lease had already
// expired or another holder has the mutex; the other holder keeps it.
func (l *Lease) Release(ctx context.Context) error {
	ok, err := releaseScript.Run(ctx, l.mutex.rdb, []string{l.mutex.leaseKey}, l.token).Int64()
	if err != nil {
		return fmt.Errorf("redisx: release lease on %s: %w", l.mutex.name, err)
	}
	if ok == 0 {
		return fmt.Errorf("redisx: release lease on %s: %w", l.mutex.name, ErrLeaseLost)
	}
	return nil
}

// run keeps the lease alive while fn runs, then releases it, also when fn
// panics.
func (l *Lease) run(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	fnCtx, cancel := context.WithCancelCause(ctx)
	stop := make(chan struct{})
	var keeper sync.WaitGroup
	var lost error
	keeper.Go(func() { lost = l.keepAlive(fnCtx, stop, cancel) })
	defer func() {
		close(stop)
		keeper.Wait()
		cancel(nil)
		releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(ctx), releaseBudget)
		defer cancelRelease()
		releaseErr := l.Release(releaseCtx)
		if lost != nil {
			err = errors.Join(err, lost)
			return
		}
		err = errors.Join(err, releaseErr)
	}()
	return fn(fnCtx)
}

// keepAlive refreshes the lease every third of the TTL until stop closes. It
// gives up, cancels fn and returns the reason when the lease is lost, or when
// no refresh has succeeded for a whole TTL.
func (l *Lease) keepAlive(ctx context.Context, stop <-chan struct{}, cancel context.CancelCauseFunc) error {
	ticker := time.NewTicker(max(l.mutex.ttl/3, time.Millisecond))
	defer ticker.Stop()
	lastRefresh := time.Now()
	for {
		select {
		case <-stop:
			return nil
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		err := l.Refresh(ctx)
		switch {
		case err == nil:
			lastRefresh = time.Now()
			continue
		case errors.Is(err, ErrLeaseLost):
		case time.Since(lastRefresh) < l.mutex.ttl:
			continue
		default:
			err = fmt.Errorf("%w: no refresh succeeded for %s: %w", ErrLeaseLost, l.mutex.ttl, err)
		}
		cancel(ErrLeaseLost)
		return err
	}
}
