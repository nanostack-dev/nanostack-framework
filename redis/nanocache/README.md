# redis/nanocache (deprecated)

Moved to [`redis/nanoredis/cache`](../nanoredis/cache/README.md). This package keeps every name as an alias of the same name there, and its own `Module`, for one release, so a service builds unchanged while it moves its imports. Replace `redis/nanocache` with `redis/nanoredis/cache` and `nanocache.` with `cache.`, then run `go tool nanostack gen`.
