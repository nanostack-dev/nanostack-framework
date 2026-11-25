# redis/nanolimit

The nano module for limits that every replica of a service shares through Redis: a per-key rate limiter and a per-key semaphore with leased permits. It has no fx predecessor.

- **Provides:** `*Limits` from `NewLimits(ctx, nanocache.Store, *slog.Logger)`, over the client of the [`redis/nanocache`](../nanocache/README.md) store (list that module first). Outside the module, `New(*redis.Client, ...Option)` and `FromStore(nanocache.Store, ...Option)` build one. A nil client (the no-op store) is valid: see **Without Redis**.
- **Rate limiter:** `limits.RateLimiter(name, Rate)` returns a `RateLimiter`. `Allow(ctx, key)` and `AllowN(ctx, key, n)` return a `Decision`: `Allowed`, `Remaining`, `RetryAfter` (refused: the wait until the same request passes) and `ResetAfter` (the wait until the key has its full burst again). `Rate{Limit, Period, Burst}` spaces events `Period/Limit` apart and lets an idle key send `Burst` (default `Limit`) at once; `PerSecond`, `PerMinute`, `PerHour` and `WithBurst` build one. `AllowN` admits all n events or none; n above the burst is an `ErrInvalidArgument`.
- **Semaphore:** `limits.Semaphore(name, permits)` returns a `Semaphore`. `Acquire(ctx, key, lease)` returns a `Permit`: `Acquired`, `ID`, `Available` and, when every permit is held, `RetryAfter` (until the earliest lease ends). `Release(ctx, key, id)` frees it; `Extend(ctx, key, id, lease)` restarts its lease; `Available(ctx, key)` reads the free permits. A permit that is not released ends with its lease, so a crashed holder frees its permit without a sweeper. Release and Extend return `ErrPermitNotHeld` for a permit released already or whose lease ended, so a late release never frees the permit of the next holder.
- **Without Redis:** `WithFailurePolicy(FailOpen)` (the default) admits the event and grants the permit, `FailClosed` refuses both. The call returns that answer together with an error that wraps `ErrUnavailable` (no client, a Redis error, or the deadline). A permit granted this way has no ID; `Release` and `Extend` of the empty ID do nothing. `Available` is a reading and returns only the error. `WithLogger` logs one warning per minute per `Limits` when a call falls back; `NewLimits` sets it and warns once at start when there is no Redis.
- **Deadline:** `WithTimeout(d)` (default 100ms) bounds every call, including the wait for a silent Redis, whether or not the client applies context deadlines. The caller's context bounds it too.
- **Options:** given to `New`/`FromStore`, they apply to every limiter built from that `Limits`; given to `RateLimiter` or `Semaphore`, they apply on top for that limiter only.
- **Config:** none. The Redis connection is the `nanocache` configuration.

## Keys and storage

A limiter of name `n` stores key `k` under the Redis key `n:k`. Give each limit its own name: two limiters with one name share their counts. A name is non-empty, without a glob character or a trailing `:`; keep names from nesting (`a` and `a:b`). The rate and the permit count are sent with every call, so a new value applies at once.

Each call is one Lua script on one key, so it is atomic across replicas and works on a Redis Cluster. The scripts read the clock of the Redis server, so the clocks of the replicas do not matter.

- **Rate:** the key holds one integer, the time (µs) at which the key has its full burst again. An admitted event moves it one spacing later; events are admitted while it stays within `Burst` spacings of now. Refused events are not counted. The key expires when it reaches that time, so an idle key holds nothing.
- **Semaphore:** the key is a sorted set of permit IDs scored by the end of their lease (ms). Every call first drops the ended leases, then the key is set to expire with its longest lease, and it is deleted with its last permit.

## Tests

`go test ./redis/nanolimit/` covers the configuration, both failure policies without Redis and against a closed port, the deadline against a server that never answers, and the throttled warning. The integration suite runs against the shared server of `redis/internal/redistest` (Docker, or `REDIS_TEST_ADDR`): burst, retry-after, rate over time, all-or-nothing `AllowN`, concurrent clients, key expiry, lease reclaim, double and late release, and `Extend`.

```sh
go test -tags=integration -race -count=1 ./redis/nanolimit/
REDIS_TEST_IMAGE=redis:7-alpine go test -tags=integration -count=1 ./redis/nanolimit/
```
