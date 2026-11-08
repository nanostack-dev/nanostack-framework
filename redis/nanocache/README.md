# redis/nanocache

The nano module for the shared cache. It replaces the fx module `modules/cache`.

- **Provides:** `Store` from `NewStore(ctx, Config, *slog.Logger)`. Services use the typed `Cache[T]` from `New[T](store, prefix, ttl, logger)`, which returns `ErrInvalidPrefix` for an empty prefix or a prefix with a glob character or a trailing `:` (two caches must not have nested prefixes such as `a` and `a:b`): it owns a key namespace and the JSON encoding, and it degrades to the loader when Redis fails.
- **Store:** the Redis store when `address` is set and Redis answers a ping within 5 seconds. Otherwise the no-op store, which caches nothing, as the fx module does: a per-instance cache would keep entries that no other instance can evict.
- **Lifecycle:** the Redis store is a `Stopper` (closes the client) and a `HealthChecker` (pings). The no-op store has no lifecycle.
- **Config** (section chosen by the service; echopoint uses `redis`, which keeps `REDIS_ADDRESS`, `REDIS_PASSWORD`, `REDIS_DB`): `address`, `password` (secret), `db`. All optional.

- **Keys:** `prefix:part:part`. The prefix is used as given. Each part may be free text: its `:` and `\` are escaped (`\:`, `\\`), so `Key("a:b")` and `Key("a", "b")` are two keys. `EvictPrefix` escapes the glob characters of the key, so a part such as `*` or `team[ops]` evicts only its own scope.

`Store` has the methods of `modules/cache.Store`. `EvictPattern` uses `SCAN`, not `KEYS`, and unlinks each page of matches as it arrives. Run the integration test with Docker: `go test -tags=integration ./redis/nanocache/` (or set `REDIS_TEST_ADDR`). See [docs/modules.md](../../docs/modules.md) for the migration from fx.
