// Package nanolimit is the nano module for limits shared by every replica of
// a service through Redis: a RateLimiter that admits a number of events per
// period for each key, and a Semaphore that leases a fixed number of permits
// for each key.
//
// Both keep their whole state in Redis and change it in one server-side
// script per call, so concurrent replicas never count the same event twice
// and a crashed replica leaves nothing behind: a rate key expires when its
// key is idle again, and a permit that is not released ends with its lease.
//
// A service lists Module in its manifest, after redis/nanocache.Module, and
// builds its limiters from the provided *Limits:
//
//	calls, err := limits.RateLimiter("mcp:calls", nanolimit.PerMinute(120))
//	decision, err := calls.Allow(ctx, userID)
//	if !decision.Allowed {
//		// 429, Retry-After: decision.RetryAfter
//	}
//
// A limit is a guard, so its behavior without Redis is an explicit choice:
// FailOpen (the default) admits the event, FailClosed refuses it. Either way
// the call returns the policy's answer together with an error that wraps
// ErrUnavailable, and no call waits longer than its deadline (WithTimeout).
package nanolimit
