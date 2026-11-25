// Package nanoredis groups the Redis integrations of the framework and the
// options they share:
//
//   - cache: the nano module of the shared cache and its Store, the Redis
//     connection the other packages build on.
//   - throttle: a per-key rate limiter shared by every replica.
//   - semaphore: N leased permits per key shared by every replica; a permit
//     that is not released ends with its lease.
//
// throttle and semaphore keep their whole state in Redis and change it in
// one server-side script per call, so concurrent replicas never count the
// same event twice. A limit is a guard, so its behavior without Redis is an
// explicit choice: FailOpen (the default) admits, FailClosed refuses. Either
// way the call returns the policy's answer together with an error that wraps
// ErrUnavailable, and no call waits longer than its deadline (WithTimeout).
//
// Redis is a trademark of Redis Ltd. nanoredis is not affiliated with or
// endorsed by Redis Ltd.
package nanoredis
