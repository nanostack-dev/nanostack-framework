// Copyright 2026 Nanostack.
//
// Ported from Redisson's RedissonRateLimiter, Copyright (c) 2013-2026 Nikita
// Koksharov, licensed under the Apache License, Version 2.0
// (http://www.apache.org/licenses/LICENSE-2.0). Nanostack changed it: the
// Java API became a Go interface, the Lua scripts are unchanged, see doc.go.

package nanolimit

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// RateType is RRateLimiter's RateType.
type RateType int

const (
	// Overall is one budget shared by every client.
	Overall RateType = 0
	// PerClient is one budget per Limits (WithClientID).
	PerClient RateType = 1
)

// RateConfig is the rate of a limiter: Rate permits per Interval.
type RateConfig struct {
	Type     RateType
	Rate     int64
	Interval time.Duration
	// KeepAlive expires the limiter's keys this long after its last use
	// (each acquire renews it). Zero keeps them until they are deleted; a
	// limiter per user should set it. It must be zero or at least Interval.
	KeepAlive time.Duration
	// KeepState, for SetRate and UpdateRate only, keeps the permits used in
	// the current interval instead of starting with a full budget. A change
	// of Type always starts with a full budget.
	KeepState bool
}

// RateLimiter is Redisson's RRateLimiter: a sliding window of Rate permits
// per Interval, kept in Redis and shared by every process.
//
// An acquire records its permits with the time it took them; a permit comes
// back Interval after it was taken. Each call sends one script.
type RateLimiter interface {
	// TrySetRate sets the rate when none is set, and reports whether it did.
	TrySetRate(ctx context.Context, cfg RateConfig) (bool, error)
	// SetRate replaces the rate (Redisson's setRate(RateLimiterArgs)).
	SetRate(ctx context.Context, cfg RateConfig) error
	// UpdateRate replaces the rate only when one is set, and reports whether
	// it did.
	UpdateRate(ctx context.Context, cfg RateConfig) (bool, error)
	// Config returns the rate, or ErrNotInitialized.
	Config(ctx context.Context) (RateConfig, error)

	// TryAcquire takes permits now, or reports false.
	TryAcquire(ctx context.Context, permits int64) (bool, error)
	// TryAcquireWait takes permits, waiting up to timeout for them.
	TryAcquireWait(ctx context.Context, permits int64, timeout time.Duration) (bool, error)
	// Acquire takes permits, waiting until they come back or ctx ends.
	Acquire(ctx context.Context, permits int64) error
	// Release gives permits back before their interval ends, up to Rate.
	Release(ctx context.Context, permits int64) error
	// AvailablePermits returns the permits that an acquire could take now.
	AvailablePermits(ctx context.Context) (int64, error)

	// Expire sets a time to live on every key of the limiter.
	Expire(ctx context.Context, ttl time.Duration) (bool, error)
	// Delete removes the limiter, and reports whether it existed.
	Delete(ctx context.Context) (bool, error)
}

type rateLimiter struct {
	limits *Limits
	name   string
}

// The five keys of every script: the config hash, the shared value and
// permits, and this client's value and permits.
func (r *rateLimiter) keys() []string {
	value := suffixName(r.name, "value")
	permits := suffixName(r.name, "permits")
	return []string{
		r.name, value, suffixName(value, r.limits.clientID), permits, suffixName(permits, r.limits.clientID),
	}
}

func validateRate(cfg RateConfig) error {
	switch {
	case cfg.Rate <= 0:
		return fmt.Errorf("%w: rate must be positive", ErrInvalidArgument)
	case cfg.Interval <= 0:
		return fmt.Errorf("%w: rate interval must be positive", ErrInvalidArgument)
	case cfg.KeepAlive < 0 || (cfg.KeepAlive != 0 && cfg.KeepAlive.Milliseconds() < cfg.Interval.Milliseconds()):
		return fmt.Errorf("%w: keep-alive must be zero or at least the rate interval", ErrInvalidArgument)
	case cfg.Type != Overall && cfg.Type != PerClient:
		return fmt.Errorf("%w: unknown rate type %d", ErrInvalidArgument, cfg.Type)
	}
	return nil
}

func (r *rateLimiter) TrySetRate(ctx context.Context, cfg RateConfig) (bool, error) {
	if err := validateRate(cfg); err != nil {
		return false, err
	}
	reply, err := r.limits.eval(ctx, trySetRateScript, []string{r.name},
		cfg.Rate, cfg.Interval.Milliseconds(), int(cfg.Type), cfg.KeepAlive.Milliseconds())
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func (r *rateLimiter) setOrUpdate(ctx context.Context, cfg RateConfig, requireExist bool) (bool, error) {
	if err := validateRate(cfg); err != nil {
		return false, err
	}
	reply, err := r.limits.eval(ctx, setOrUpdateRateScript, r.keys(),
		cfg.Rate, cfg.Interval.Milliseconds(), int(cfg.Type), cfg.KeepAlive.Milliseconds(),
		nowMillis(), boolArg(cfg.KeepState), boolArg(requireExist))
	if err != nil {
		return false, err
	}
	n, err := toInt64(reply)
	return n == 1, err
}

func boolArg(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *rateLimiter) SetRate(ctx context.Context, cfg RateConfig) error {
	_, err := r.setOrUpdate(ctx, cfg, false)
	return err
}

func (r *rateLimiter) UpdateRate(ctx context.Context, cfg RateConfig) (bool, error) {
	return r.setOrUpdate(ctx, cfg, true)
}

func (r *rateLimiter) Config(ctx context.Context) (RateConfig, error) {
	if r.limits.rdb == nil {
		return RateConfig{}, fmt.Errorf("%w: no redis client", ErrUnavailable)
	}
	fields, err := r.limits.rdb.HGetAll(ctx, r.name).Result()
	if err != nil {
		return RateConfig{}, classify(ctx, err)
	}
	if len(fields) == 0 {
		return RateConfig{}, ErrNotInitialized
	}
	var cfg RateConfig
	var n int64
	for field, target := range map[string]*int64{"rate": &cfg.Rate, "interval": &n} {
		if *target, err = strconv.ParseInt(fields[field], 10, 64); err != nil {
			return RateConfig{}, fmt.Errorf("nanolimit: parse %s of %s: %w", field, r.name, err)
		}
	}
	cfg.Interval = time.Duration(n) * time.Millisecond
	typ, err := strconv.Atoi(fields["type"])
	if err != nil {
		return RateConfig{}, fmt.Errorf("nanolimit: parse type of %s: %w", r.name, err)
	}
	cfg.Type = RateType(typ)
	if keepAlive, parseErr := strconv.ParseInt(fields["keepAliveTime"], 10, 64); parseErr == nil {
		cfg.KeepAlive = time.Duration(keepAlive) * time.Millisecond
	}
	return cfg, nil
}

// try sends one acquire: (0, nil) took the permits, (delay, nil) did not.
func (r *rateLimiter) try(ctx context.Context, permits int64) (time.Duration, error) {
	reply, err := r.limits.eval(ctx, tryAcquireScript, r.keys(), permits, nowMillis(), newIDBytes())
	if err != nil {
		return 0, err
	}
	if reply == nil {
		return 0, nil
	}
	delay, err := toInt64(reply)
	if err != nil {
		return 0, err
	}
	// A permit already due comes back on the next try; never spin.
	return max(time.Duration(delay)*time.Millisecond, time.Millisecond), nil
}

func validatePermits(permits int64) error {
	if permits < 0 {
		return fmt.Errorf("%w: permits must not be negative", ErrInvalidArgument)
	}
	return nil
}

func (r *rateLimiter) TryAcquire(ctx context.Context, permits int64) (bool, error) {
	if err := validatePermits(permits); err != nil {
		return false, err
	}
	delay, err := r.try(ctx, permits)
	if err != nil {
		return r.admitOnFailure(ctx, "rate.try_acquire", err)
	}
	return delay == 0, nil
}

func (r *rateLimiter) TryAcquireWait(ctx context.Context, permits int64, timeout time.Duration) (bool, error) {
	if err := validatePermits(permits); err != nil {
		return false, err
	}
	return r.wait(ctx, permits, time.Now().Add(timeout), true)
}

func (r *rateLimiter) Acquire(ctx context.Context, permits int64) error {
	if err := validatePermits(permits); err != nil {
		return err
	}
	_, err := r.wait(ctx, permits, time.Time{}, false)
	return err
}

// wait retries the acquire after the delay the script returns, as Redisson
// does, until it succeeds, the deadline passes (bounded) or ctx ends. Like
// Redisson it gives up early when the next permit comes back after the
// deadline, once the remaining time has passed.
func (r *rateLimiter) wait(ctx context.Context, permits int64, deadline time.Time, bounded bool) (bool, error) {
	for {
		delay, err := r.try(ctx, permits)
		if err != nil {
			return r.admitOnFailure(ctx, "rate.acquire", err)
		}
		if delay == 0 {
			return true, nil
		}
		pause, last := delay, false
		if bounded {
			remains := time.Until(deadline)
			if remains <= 0 {
				return false, nil
			}
			if remains < delay {
				pause, last = remains, true
			}
		}
		if err = sleepCtx(ctx, pause); err != nil || last {
			return false, err
		}
	}
}

// admitOnFailure applies the failure policy to a failed acquire.
func (r *rateLimiter) admitOnFailure(ctx context.Context, op string, err error) (bool, error) {
	if err = r.limits.degrade(ctx, op, r.name, err); err != nil {
		return false, err
	}
	return true, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("nanolimit: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *rateLimiter) Release(ctx context.Context, permits int64) error {
	if err := validatePermits(permits); err != nil {
		return err
	}
	if permits == 0 {
		return nil
	}
	_, err := r.limits.eval(ctx, releaseRateScript, r.keys(), permits)
	return err
}

func (r *rateLimiter) AvailablePermits(ctx context.Context) (int64, error) {
	reply, err := r.limits.eval(ctx, availableRatePermitsScript, r.keys(), nowMillis())
	if err != nil {
		return 0, err
	}
	return toInt64(reply)
}

func (r *rateLimiter) Expire(ctx context.Context, ttl time.Duration) (bool, error) {
	return r.limits.expire(ctx, ttl, r.keys()...)
}

func (r *rateLimiter) Delete(ctx context.Context) (bool, error) {
	return r.limits.del(ctx, r.keys()...)
}
