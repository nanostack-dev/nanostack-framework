# redis/nanoredis

The framework's Redis integrations, in one tree so they share their connection and their guard code:

| Package | What it is |
| --- | --- |
| [`cache`](cache/README.md) | The nano module of the shared cache (`cache.Module`, `cache.Store`, typed `cache.Cache[T]`). Its `Store` is the Redis connection the other packages build on. Formerly `redis/nanocache`, which now forwards here for one release. |
| [`throttle`](#throttle) | A per-key rate limiter shared by every replica. |
| [`semaphore`](#semaphore) | Up to N leased permits per key shared by every replica; a permit that is not released ends with its lease. |

`internal/guard` holds what `throttle` and `semaphore` share: the client of a store, name and key validation, the failure policy, the per-call deadline and the throttled warning. Package `nanoredis` re-exports its options and errors.

Redis is a trademark of Redis Ltd. nanoredis is not affiliated with or endorsed by Redis Ltd.

## Shared behavior of throttle and semaphore

- **Construction:** `throttle.New(store, name, rate, opts...)` and `semaphore.New(store, name, permits, opts...)` take the `cache.Store` that the service already has; `NewWithClient` takes a `*redis.Client` the service owns. The no-op store and a nil client are valid: see **Without Redis**. Neither package is a nano module: a service builds its limiters in its own constructors from the `Store` that `cache.Module` provides.
- **Names and keys:** a limiter of name `n` stores key `k` under the Redis key `n:k`. Give each limit its own name: two limiters with one name share their counts. A name is non-empty, without a glob character or a trailing `:`; keep names from nesting (`a` and `a:b`). The rate and the permit count are sent with every call, so a new value applies at once.
- **Atomicity:** each call is one Lua script on one key, so it is atomic across replicas and works on a Redis Cluster. The scripts read the clock of the Redis server, so the clocks of the replicas do not matter.
- **Without Redis:** `nanoredis.WithFailurePolicy(nanoredis.FailOpen)` (the default) admits the event and grants the permit, `FailClosed` refuses both. The call returns that answer together with an error that wraps `nanoredis.ErrUnavailable` (no client, a Redis error, or the deadline). `nanoredis.WithLogger` logs one warning per minute per limiter when a call falls back.
- **Deadline:** `nanoredis.WithTimeout(d)` (default 100ms) bounds every call, including the wait for a Redis that accepts connections and never answers, whether or not the client applies context deadlines (`cache.NewStore` builds a client that does not). The caller's context bounds it too.
- **Invalid arguments** (an empty key, n outside 1..burst, a lease under 1ms) return a refusal and an error that wraps `nanoredis.ErrInvalidArgument`; no policy applies.

## throttle

`throttle.Limiter` has `Allow(ctx, key)` and `AllowN(ctx, key, n)`, which return a `Decision`: `Allowed`, `Remaining`, `RetryAfter` (refused: the wait until the same request passes) and `ResetAfter` (the wait until the key has its full burst again). `Rate{Limit, Period, Burst}` spaces events `Period/Limit` apart and lets an idle key send `Burst` (default `Limit`) at once; `PerSecond`, `PerMinute`, `PerHour` and `WithBurst` build one. `AllowN` admits all n events or none. Refused events are not counted.

The key holds one integer: the time (µs) at which the key has its full burst again. An admitted event moves it one spacing later; events are admitted while it stays within `Burst` spacings of now. The key expires when it reaches that time, so an idle key holds nothing. Because the key stores a time, a new rate applies from the next call and the time a key already spent carries over.

## semaphore

`semaphore.Pool` has `Acquire(ctx, key, lease)`, which returns a `Permit`: `Acquired`, `ID`, `Available` and, when every permit is held, `RetryAfter` (until the earliest lease ends). `Release(ctx, key, id)` frees it; `Extend(ctx, key, id, lease)` restarts its lease; `Available(ctx, key)` reads the free permits (a reading: no policy applies). A permit that is not released ends with its lease, so a crashed holder frees its permit without a sweeper. `Release` and `Extend` return `semaphore.ErrPermitNotHeld` for a permit released already or whose lease ended, so a late release never frees the permit of the next holder. A permit granted by `FailOpen` without Redis has no ID; `Release` and `Extend` of the empty ID do nothing.

The key is a sorted set of permit IDs scored by the end of their lease (ms). Every call first drops the ended leases, then the key is set to expire with its longest lease, and it is deleted with its last permit.

## Tests

`go test ./redis/nanoredis/...` covers, without Redis, the configuration, both failure policies with no client and against a closed port, the deadline against a server that never answers, and the throttled warning. The integration suites run against the shared server of `redis/internal/redistest` (Docker, or `REDIS_TEST_ADDR`): burst, retry-after, rate over time, all-or-nothing `AllowN`, concurrent clients, key expiry, lease reclaim, double and late release, and `Extend`.

```sh
go test -tags=integration -race -count=1 ./redis/...
REDIS_TEST_IMAGE=redis:7-alpine go test -tags=integration -count=1 ./redis/nanoredis/...
```
