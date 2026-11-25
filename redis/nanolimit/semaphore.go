// Copyright 2026 Nanostack.
//
// Ported from Redisson's RedissonPermitExpirableSemaphore, Copyright (c)
// 2013-2026 Nikita Koksharov, licensed under the Apache License, Version 2.0
// (http://www.apache.org/licenses/LICENSE-2.0). Nanostack changed it: the
// Java API became a Go interface, permit IDs are stored as hex text, the
// waits poll the pub/sub channel with a context; see doc.go.

package nanolimit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// NoExpiry is the lease of a permit that never expires: pass it (or any
// lease <= 0) to an acquire, and LeaseTime returns it for such a permit.
const NoExpiry time.Duration = -1

const (
	// nonExpirableTimeout is Redisson's score of a permit without lease.
	nonExpirableTimeout int64 = 922337203685477
	publishCommand            = "publish"
	// waitSafetyPoll bounds one wait for a release message, so a lost
	// message costs this long at most.
	waitSafetyPoll = 5 * time.Second
	// waitPoll spaces the tries when the subscription failed.
	waitPoll = 100 * time.Millisecond
)

// Semaphore is Redisson's RPermitExpirableSemaphore: a counting semaphore in
// Redis whose permits each have an ID and, optionally, a lease after which
// the permit comes back by itself. A crashed holder never keeps a permit
// with a lease.
//
// The count must be set first (TrySetPermits or SetPermits); an acquire on a
// semaphore without a count never succeeds. An acquire returns the permit
// IDs, which the holder passes to Release.
type Semaphore interface {
	// TrySetPermits sets the count when none is set, and reports whether it
	// did.
	TrySetPermits(ctx context.Context, permits int) (bool, error)
	// SetPermits sets the total of the permits: held plus available. A total
	// below the held permits leaves the available count negative.
	SetPermits(ctx context.Context, permits int) error
	// AddPermits adds permits (or removes them, when negative).
	AddPermits(ctx context.Context, permits int) error

	// TryAcquire takes one permit now, or reports false.
	TryAcquire(ctx context.Context, lease time.Duration) (string, bool, error)
	// TryAcquireN takes n permits now, or none and returns nil.
	TryAcquireN(ctx context.Context, n int, lease time.Duration) ([]string, error)
	// TryAcquireWait takes one permit, waiting up to wait for it.
	TryAcquireWait(ctx context.Context, wait, lease time.Duration) (string, bool, error)
	// TryAcquireNWait takes n permits, waiting up to wait for them.
	TryAcquireNWait(ctx context.Context, n int, wait, lease time.Duration) ([]string, error)
	// Acquire takes one permit, waiting until one is free or ctx ends.
	Acquire(ctx context.Context, lease time.Duration) (string, error)
	// AcquireN takes n permits, waiting until they are free or ctx ends.
	AcquireN(ctx context.Context, n int, lease time.Duration) ([]string, error)

	// Release gives a permit back, or returns ErrPermitNotFound.
	Release(ctx context.Context, id string) error
	// ReleaseN gives the permits back. When one of them is unknown or
	// expired it releases none and returns ErrPermitNotFound.
	ReleaseN(ctx context.Context, ids []string) error
	// TryRelease gives a permit back and reports whether it was still held.
	// An expired permit is reclaimed and reports false.
	TryRelease(ctx context.Context, id string) (bool, error)
	// TryReleaseN gives the permits back and returns how many it released:
	// zero, without releasing any, when one is unknown or expired.
	TryReleaseN(ctx context.Context, ids []string) (int, error)

	// AvailablePermits returns the permits that are free now.
	AvailablePermits(ctx context.Context) (int64, error)
	// Permits returns the total: held plus available.
	Permits(ctx context.Context) (int64, error)
	// AcquiredPermits returns the held permits.
	AcquiredPermits(ctx context.Context) (int64, error)
	// UpdateLeaseTime gives a held permit a new lease from now, and reports
	// whether the permit was still held.
	UpdateLeaseTime(ctx context.Context, id string, lease time.Duration) (bool, error)
	// LeaseTime returns the lease left on a held permit, NoExpiry, or
	// ErrPermitNotFound.
	LeaseTime(ctx context.Context, id string) (time.Duration, error)

	// Expire sets a time to live on the semaphore's keys. An acquire gives
	// the permits the same time to live.
	Expire(ctx context.Context, ttl time.Duration) (bool, error)
	// Delete removes the semaphore, and reports whether it existed.
	Delete(ctx context.Context) (bool, error)
}

type semaphore struct {
	limits      *Limits
	name        string
	timeoutName string
	channelName string
}

func timeoutDate(lease time.Duration) int64 {
	if lease <= 0 {
		return nonExpirableTimeout
	}
	return nowMillis() + lease.Milliseconds()
}

// try sends one acquire. It returns the IDs, or nil and the time until the
// earliest lease ends (0: no lease to wait for).
func (s *semaphore) try(ctx context.Context, n int, lease time.Duration) ([]string, time.Duration, error) {
	ids := make([]string, n)
	args := make([]any, 0, 5+n)
	args = append(args, n, timeoutDate(lease), nowMillis(), nonExpirableTimeout, publishCommand)
	for i := range ids {
		ids[i] = newID()
		args = append(args, ids[i])
	}
	reply, err := s.limits.eval(ctx, tryAcquireSemaphoreScript, []string{s.name, s.timeoutName, s.channelName}, args...)
	if err != nil {
		// The reply may be lost after the script ran: give the IDs back, as
		// Redisson does.
		s.releaseLost(ctx, ids)
		return nil, 0, err
	}
	result, _ := reply.(string)
	switch {
	case result == "OK":
		return ids, 0, nil
	case strings.HasPrefix(result, ":"):
		score, parseErr := strconv.ParseFloat(result[1:], 64)
		if parseErr != nil {
			return nil, 0, fmt.Errorf("nanolimit: parse lease end %q: %w", result, parseErr)
		}
		return nil, max(time.Duration(int64(score)-nowMillis())*time.Millisecond, time.Millisecond), nil
	default:
		return nil, 0, nil
	}
}

// releaseLost releases, in the background as Redisson does, the IDs of an
// acquire whose reply did not arrive: the script may have run. An ID that
// was never added is a no-op.
func (s *semaphore) releaseLost(ctx context.Context, ids []string) {
	if s.limits.rdb == nil {
		return
	}
	go func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		for _, id := range ids {
			if _, err := s.TryRelease(releaseCtx, id); err != nil {
				return // Redis is still unreachable; a lease, if any, frees the permit.
			}
		}
	}()
}

func validateN(n int) error {
	if n < 0 {
		return fmt.Errorf("%w: permits must not be negative", ErrInvalidArgument)
	}
	return nil
}

// degradeAcquire applies the failure policy: FailOpen returns stand-in
// permits, whose release is a no-op.
func (s *semaphore) degradeAcquire(ctx context.Context, op string, n int, err error) ([]string, error) {
	if err = s.limits.degrade(ctx, op, s.name, err); err != nil {
		return nil, err
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = unavailablePermitPrefix + newID()
	}
	return ids, nil
}

func (s *semaphore) TryAcquireN(ctx context.Context, n int, lease time.Duration) ([]string, error) {
	if err := validateN(n); err != nil {
		return nil, err
	}
	ids, _, err := s.try(ctx, n, lease)
	if err != nil {
		return s.degradeAcquire(ctx, "semaphore.try_acquire", n, err)
	}
	return ids, nil
}

func (s *semaphore) TryAcquire(ctx context.Context, lease time.Duration) (string, bool, error) {
	return first(s.TryAcquireN(ctx, 1, lease))
}

func first(ids []string, err error) (string, bool, error) {
	if err != nil || len(ids) == 0 {
		return "", false, err
	}
	return ids[0], true, nil
}

func (s *semaphore) TryAcquireNWait(ctx context.Context, n int, wait, lease time.Duration) ([]string, error) {
	if err := validateN(n); err != nil {
		return nil, err
	}
	return s.wait(ctx, n, lease, time.Now().Add(wait), true)
}

func (s *semaphore) TryAcquireWait(ctx context.Context, wait, lease time.Duration) (string, bool, error) {
	return first(s.TryAcquireNWait(ctx, 1, wait, lease))
}

func (s *semaphore) AcquireN(ctx context.Context, n int, lease time.Duration) ([]string, error) {
	if err := validateN(n); err != nil {
		return nil, err
	}
	return s.wait(ctx, n, lease, time.Time{}, false)
}

func (s *semaphore) Acquire(ctx context.Context, lease time.Duration) (string, error) {
	id, _, err := first(s.AcquireN(ctx, 1, lease))
	return id, err
}

// wait is Redisson's acquire loop: try, then wait for a release message on
// the semaphore's channel or for the earliest lease to end, and try again.
// It returns nil when the deadline passes (bounded).
func (s *semaphore) wait(
	ctx context.Context, n int, lease time.Duration, deadline time.Time, bounded bool,
) ([]string, error) {
	ids, _, err := s.try(ctx, n, lease)
	if err != nil {
		return s.degradeAcquire(ctx, "semaphore.acquire", n, err)
	}
	if ids != nil {
		return ids, nil
	}
	if bounded && !time.Now().Before(deadline) {
		return nil, nil
	}
	released, closeSub := s.subscribe(ctx)
	defer closeSub()
	for {
		var nearest time.Duration
		ids, nearest, err = s.try(ctx, n, lease)
		if err != nil {
			return s.degradeAcquire(ctx, "semaphore.acquire", n, err)
		}
		if ids != nil {
			return ids, nil
		}
		pause := waitSafetyPoll
		if released == nil {
			pause = waitPoll
		}
		if nearest > 0 {
			pause = min(pause, nearest)
		}
		if bounded {
			remains := time.Until(deadline)
			if remains <= 0 {
				return nil, nil
			}
			pause = min(pause, remains)
		}
		if err = s.pause(ctx, released, pause); err != nil {
			return nil, err
		}
	}
}

// subscribe listens on the semaphore's channel before the next try, so a
// release between the try and the wait still wakes it. On failure it
// returns a nil channel and the wait polls.
func (s *semaphore) subscribe(ctx context.Context) (<-chan *redis.Message, func()) {
	if s.limits.rdb == nil {
		return nil, func() {}
	}
	sub := s.limits.rdb.Subscribe(ctx, s.channelName)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, func() {}
	}
	return sub.Channel(), func() { _ = sub.Close() }
}

func (s *semaphore) pause(ctx context.Context, released <-chan *redis.Message, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("nanolimit: %w", ctx.Err())
	case <-released:
		return nil
	case <-timer.C:
		return nil
	}
}

func (s *semaphore) TryRelease(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("%w: empty permit id", ErrInvalidArgument)
	}
	if IsStandIn(id) {
		return true, nil
	}
	reply, err := s.limits.eval(ctx, tryReleaseSemaphoreScript,
		[]string{s.name, s.channelName, s.timeoutName}, id, 1, nowMillis(), publishCommand)
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func (s *semaphore) Release(ctx context.Context, id string) error {
	released, err := s.TryRelease(ctx, id)
	if err != nil {
		return err
	}
	if !released {
		return fmt.Errorf("%w: %s", ErrPermitNotFound, id)
	}
	return nil
}

func (s *semaphore) TryReleaseN(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: no permit ids", ErrInvalidArgument)
	}
	standIns := 0
	args := []any{len(ids), nowMillis(), publishCommand}
	for _, id := range ids {
		if IsStandIn(id) {
			standIns++
			continue
		}
		args = append(args, id)
	}
	if standIns == len(ids) {
		return standIns, nil
	}
	reply, err := s.limits.eval(ctx, tryReleaseManySemaphoreScript,
		[]string{s.name, s.channelName, s.timeoutName}, args...)
	if err != nil {
		return 0, err
	}
	n, err := toInt64(reply)
	if err != nil || n == 0 {
		return 0, err
	}
	return int(n) + standIns, nil
}

func (s *semaphore) ReleaseN(ctx context.Context, ids []string) error {
	released, err := s.TryReleaseN(ctx, ids)
	if err != nil {
		return err
	}
	if released != len(ids) {
		return fmt.Errorf("%w: %d of %d permits", ErrPermitNotFound, len(ids)-released, len(ids))
	}
	return nil
}

func (s *semaphore) count(ctx context.Context, script *redis.Script) (int64, error) {
	reply, err := s.limits.eval(ctx, script,
		[]string{s.name, s.timeoutName, s.channelName}, nowMillis(), publishCommand)
	if err != nil {
		return 0, err
	}
	return toInt64(reply)
}

func (s *semaphore) AvailablePermits(ctx context.Context) (int64, error) {
	return s.count(ctx, availableSemaphorePermitsScript)
}

func (s *semaphore) Permits(ctx context.Context) (int64, error) {
	return s.count(ctx, semaphorePermitsScript)
}

func (s *semaphore) AcquiredPermits(ctx context.Context) (int64, error) {
	return s.count(ctx, acquiredSemaphorePermitsScript)
}

func (s *semaphore) TrySetPermits(ctx context.Context, permits int) (bool, error) {
	reply, err := s.limits.eval(ctx, trySetSemaphorePermitsScript,
		[]string{s.name, s.channelName}, permits, publishCommand)
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func (s *semaphore) SetPermits(ctx context.Context, permits int) error {
	_, err := s.limits.eval(ctx, setSemaphorePermitsScript,
		[]string{s.name, s.channelName, s.timeoutName}, permits, publishCommand)
	return err
}

func (s *semaphore) AddPermits(ctx context.Context, permits int) error {
	_, err := s.limits.eval(ctx, addSemaphorePermitsScript,
		[]string{s.name, s.channelName}, permits, publishCommand)
	return err
}

func (s *semaphore) UpdateLeaseTime(ctx context.Context, id string, lease time.Duration) (bool, error) {
	if IsStandIn(id) {
		return true, nil
	}
	reply, err := s.limits.eval(ctx, updateLeaseTimeScript,
		[]string{s.name, s.timeoutName, s.channelName}, id, timeoutDate(lease), nowMillis(), publishCommand)
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func (s *semaphore) LeaseTime(ctx context.Context, id string) (time.Duration, error) {
	reply, err := s.limits.eval(ctx, leaseTimeScript,
		[]string{s.name, s.timeoutName, s.channelName}, id, nonExpirableTimeout, nowMillis(), publishCommand)
	if err != nil {
		return 0, err
	}
	n, err := toInt64(reply)
	switch {
	case err != nil:
		return 0, err
	case n == 0:
		return 0, fmt.Errorf("%w: %s", ErrPermitNotFound, id)
	case n == -1:
		return NoExpiry, nil
	default:
		return time.Duration(n) * time.Millisecond, nil
	}
}

func (s *semaphore) Expire(ctx context.Context, ttl time.Duration) (bool, error) {
	return s.limits.expire(ctx, ttl, s.name, s.timeoutName)
}

func (s *semaphore) Delete(ctx context.Context) (bool, error) {
	return s.limits.del(ctx, s.name, s.timeoutName)
}

// IsStandIn reports whether id is a stand-in permit that FailOpen returned
// while Redis was unavailable.
func IsStandIn(id string) bool { return strings.HasPrefix(id, unavailablePermitPrefix) }
