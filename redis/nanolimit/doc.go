// Package nanolimit is the nano module for distributed limits in Redis: a
// sliding-window RateLimiter and a permit-expirable Semaphore, shared by
// every replica of a service.
//
// A service lists Module in its manifest next to nanocache.Module: it
// provides *Limits over the cache's Redis client. Limits hands out a limiter
// or a semaphore by name; the name is the Redis key, so the caller owns the
// namespace (for example "mcp:calls:<user>").
//
//	limits := nanolimit.NewFromStore(store, logger)
//	calls := limits.RateLimiter("mcp:calls:" + userID)
//	ok, err := calls.TryAcquire(ctx, 1)
//	if errors.Is(err, nanolimit.ErrNotInitialized) {
//		_, _ = calls.TrySetRate(ctx, nanolimit.RateConfig{
//			Rate: 120, Interval: time.Minute, KeepAlive: 2 * time.Minute,
//		})
//		ok, err = calls.TryAcquire(ctx, 1)
//	}
//
// # Without Redis
//
// A nil client (the no-op nanocache store) and a failing Redis behave the
// same: every Redis call fails with ErrUnavailable. The FailurePolicy then
// decides an acquire. FailOpen, the default, admits: TryAcquire returns
// true, a semaphore returns stand-in permits (IsStandIn) whose release is a
// no-op, and one warning per minute is logged. FailClosed returns the error.
// Reads and configuration calls (AvailablePermits, Config, TrySetRate,
// Release, ...) always return the error. The caller's cancellation is never
// read as unavailability: a call whose ctx ended returns ctx.Err(), wrapped.
//
// # Origin
//
// The rate limiter and the semaphore are ports of Redisson's
// RedissonRateLimiter and RedissonPermitExpirableSemaphore
// (https://github.com/redisson/redisson, Apache License 2.0, Copyright (c)
// 2013-2026 Nikita Koksharov) at commit
// b019f0eb75d149a37711352db9db06f9973ca1eb. Their Lua scripts and key
// layout are unchanged, so the behaviour, the atomicity and the cluster hash
// slots are Redisson's. Redisson's tests are ported in the
// *_redisson_integration_test.go files. Nanostack changed:
//
//   - The API: Go interfaces, a context on every call, errors instead of
//     exceptions (ErrNotInitialized, ErrPermitsExceedRate,
//     ErrInvalidArgument, ErrPermitNotFound), and the failure policy above.
//   - Waiting: Acquire and the *Wait methods end with their context. The
//     rate limiter sleeps the delay its script returns, as Redisson does.
//     The semaphore subscribes to its release channel (named
//     nanolimit_sc:{name}, not redisson_sc:{name}) for each wait instead of
//     sharing one subscription, and tries again at least every 5 seconds.
//   - Permit IDs are stored as their hex text, not the decoded bytes, so the
//     keys are not interchangeable with Redisson's.
//   - Config returns ErrNotInitialized for a limiter without a rate, where
//     Redisson returns an empty config, and also returns the keep-alive.
//   - Left out: the async and reactive variants, RateLimiterArgs (its fields
//     are in RateConfig), the deprecated RateIntervalUnit overloads, and the
//     other RExpirable methods; Expire and Delete cover every key.
//
// Times are the client's clock (time.Now), as in Redisson: replicas whose
// clocks differ by d see a lease or an interval end up to d early or late.
package nanolimit
