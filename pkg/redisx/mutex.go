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
	// A release wakes waiting callers through pub/sub. Polling only covers a
	// missed notification, so it can be slow.
	maxRetryDelay = time.Second
	releaseBudget = 5 * time.Second
	// The released key lets a Release that go-redis sends again after a lost
	// reply see that the first attempt already released the lease.
	releasedTTL = time.Minute
)

// The lease, fence and released keys share a hash tag, so the scripts also run
// on Redis Cluster.
//
// go-redis sends a command again when its reply is lost, so every script that
// writes must give the same answer when it runs twice for the same token.
//
// The fence is at least the server time in microseconds. A server that lost
// its data, after a restart without persistence or a failover to a replica that
// missed the last writes, then still gives a fence greater than the fences it
// gave before.
var (
	acquireScript = redis.NewScript(`
local holder = redis.call('GET', KEYS[1])
if holder == ARGV[1] then
	return {tonumber(redis.call('GET', KEYS[2])), 0}
end
if holder then
	return {0, redis.call('PTTL', KEYS[1])}
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
local now = redis.call('TIME')
local floor = tonumber(now[1]) * 1000000 + tonumber(now[2])
local fence = redis.call('INCR', KEYS[2])
if fence < floor then
	fence = redis.call('INCRBY', KEYS[2], floor - fence)
end
return {fence, 0}`)
	refreshScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0`)
	releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	redis.call('DEL', KEYS[1])
	redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[2])
	redis.call('PUBLISH', ARGV[3], '')
	return 1
end
if redis.call('GET', KEYS[2]) == ARGV[1] then
	return 1
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
//
// The mutex is not reentrant: a Do inside a Do on the same name waits for
// itself.
type Mutex struct {
	rdb         redis.UniversalClient
	releases    *releases
	name        string
	leaseKey    string
	fenceKey    string
	releasedKey string
	channel     string
	ttl         time.Duration
	wait        time.Duration
}

// Mutex returns the mutex called name. Nothing is read or written until you
// call a method.
func (c *Client) Mutex(name string) Mutex {
	tag := "{" + c.Key(name) + "}"
	return Mutex{
		rdb:         c.rdb,
		releases:    c.releases,
		name:        name,
		leaseKey:    tag + ":lease",
		fenceKey:    tag + ":fence",
		releasedKey: tag + ":released",
		channel:     tag + ":released",
		ttl:         DefaultLeaseTTL,
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
//
// A release wakes the waiting callers at once. A lease that expires without a
// release wakes them when it expires.
func (m Mutex) Lock(ctx context.Context) (*Lease, error) {
	var deadline <-chan time.Time
	if m.wait > 0 {
		timer := time.NewTimer(m.wait)
		defer timer.Stop()
		deadline = timer.C
	}
	var released <-chan struct{}
	listening := false
	delay := minRetryDelay
	for {
		lease, holderTTL, err := m.tryLock(ctx)
		if !errors.Is(err, ErrLocked) {
			return lease, err
		}
		if !listening {
			// Listen first and try again, so that a release between the
			// first try and the subscription is not missed.
			listening = true
			var stop func()
			released, stop = m.releases.listen(ctx, m.channel)
			defer stop()
			continue
		}
		// The server reports the holder's TTL in whole milliseconds, so 0
		// means it expires within the next one.
		pause := mrand.N(delay)
		if holderTTL >= 0 {
			pause = min(pause, holderTTL)
		}
		pause += time.Millisecond
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("redisx: wait for mutex %s: %w", m.name, context.Cause(ctx))
		case <-deadline:
			timer.Stop()
			return nil, fmt.Errorf("redisx: wait %s for mutex %s: %w", m.wait, m.name, ErrLocked)
		case <-released:
			timer.Stop()
		case <-timer.C:
		}
		delay = min(2*delay, maxRetryDelay)
	}
}

// TryLock takes the mutex if it is free, and returns ErrLocked if it is not.
func (m Mutex) TryLock(ctx context.Context) (*Lease, error) {
	lease, _, err := m.tryLock(ctx)
	return lease, err
}

// tryLock also returns how long the current holder keeps the lease when the
// mutex is held.
func (m Mutex) tryLock(ctx context.Context) (*Lease, time.Duration, error) {
	lease := &Lease{mutex: m, token: rand.Text(), acquired: time.Now()}
	reply, err := acquireScript.Run(ctx, m.rdb,
		[]string{m.leaseKey, m.fenceKey}, lease.token, m.ttl.Milliseconds()).Int64Slice()
	if err == nil && len(reply) != 2 {
		err = fmt.Errorf("unexpected reply %v", reply)
	}
	if err != nil {
		// The server possibly took the lease before the reply was lost.
		// Give it back, so that the others do not wait for it to expire.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = lease.Release(releaseCtx)
		return nil, 0, fmt.Errorf("redisx: lock %s: %w", m.name, err)
	}
	if reply[0] == 0 {
		return nil, time.Duration(reply[1]) * time.Millisecond, fmt.Errorf("redisx: lock %s: %w", m.name, ErrLocked)
	}
	lease.fence = reply[0]
	return lease, 0, nil
}

// Do runs fn while it holds the mutex. It waits for the mutex like Lock,
// refreshes the lease while fn runs and releases it after. It keeps the lease
// until fn returns, also after ctx ends, so fn stays alone while it stops.
//
// If the lease is lost while fn runs, Do cancels the context it gave fn with
// ErrLeaseLost as the cause and returns an error that matches ErrLeaseLost:
// fn's work was then possibly not exclusive. Do cancels fn no later than the
// moment the lease expires on the server, also when the server stops
// answering.
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
	mutex    Mutex
	token    string
	fence    int64
	acquired time.Time
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

// Release frees the mutex and wakes the callers that wait for it. It returns
// ErrLeaseLost when the lease had already expired or another holder has the
// mutex; the other holder keeps it.
func (l *Lease) Release(ctx context.Context) error {
	ok, err := releaseScript.Run(ctx, l.mutex.rdb,
		[]string{l.mutex.leaseKey, l.mutex.releasedKey},
		l.token, releasedTTL.Milliseconds(), l.mutex.channel).Int64()
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
	keeper.Go(func() { lost = l.keepAlive(context.WithoutCancel(ctx), stop, cancel) })
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
// gives up, cancels fn and returns the reason when the server says the lease is
// lost, or when the lease expires before a refresh succeeds.
//
// A refresh runs in its own goroutine: a server that stops answering can hold
// a command for longer than the lease lasts. The expiry is counted from the
// moment the last successful refresh was sent, which is never later than the
// moment the server extended the lease.
func (l *Lease) keepAlive(ctx context.Context, stop <-chan struct{}, cancel context.CancelCauseFunc) error {
	ttl := l.mutex.ttl
	ticker := time.NewTicker(max(ttl/3, time.Millisecond))
	defer ticker.Stop()
	expiry := time.NewTimer(time.Until(l.acquired.Add(ttl)))
	defer expiry.Stop()
	type refreshed struct {
		sent time.Time
		err  error
	}
	results := make(chan refreshed, 1)
	refreshing := false
	lastErr := errors.New("the server did not answer")
	for {
		select {
		case <-stop:
			return nil
		case <-expiry.C:
			cancel(ErrLeaseLost)
			return fmt.Errorf("%w: no refresh succeeded for %s: %w", ErrLeaseLost, ttl, lastErr)
		case <-ticker.C:
			if refreshing {
				continue
			}
			refreshing = true
			sent := time.Now()
			go func() { results <- refreshed{sent: sent, err: l.Refresh(ctx)} }()
		case result := <-results:
			refreshing = false
			switch {
			case result.err == nil:
				expiry.Reset(time.Until(result.sent.Add(ttl)))
			case errors.Is(result.err, ErrLeaseLost):
				cancel(ErrLeaseLost)
				return result.err
			default:
				lastErr = result.err
			}
		}
	}
}
