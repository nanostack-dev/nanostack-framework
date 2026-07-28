# cache

`Cache[T]` is the API applications use: a type-safe cache for one kind of value,
owning a key namespace and its serialization.

```go
products := cache.New[product.Product](store, "product", 30*time.Minute, logger)

prod, err := products.Key(tenantID, productID).GetOrElse(ctx, loadFromDB)
err = products.Key(tenantID, productID).Set(ctx, prod)
err = products.Key(tenantID, productID).Evict(ctx)
err = products.EvictPrefix(ctx, tenantID)   // every entry for one tenant
```

Keys are the prefix followed by the caller's parts, joined with `:`. Hold a
`Cache[T]` as a struct field — there is no need for a per-entity cache service
wrapping it.

`GetOrElse` degrades rather than fails. If the cache cannot be read, the loader
runs anyway; if the cache cannot be written, the loaded value is still returned.
Either way the failure is logged at warn. A cache that is down should slow a
request, not break it.

An entry that will not decode counts as unreadable: the loader runs and
overwrites it, so a `T` whose shape changed between deploys cannot poison a key
until its TTL expires.

It treats a nil from the loader as "does not exist" — nothing is cached, and
`(nil, nil)` comes back — so an absent record stays distinguishable from a
failure. Signal absence only with a nil value; every error from a loader
propagates, `ErrCacheKeyNotFound` included.

`Get` and `Set` report cache errors directly, for callers that need to know.
`Set` rejects a nil value with `ErrNilValue`: storing it would encode JSON
`null`, which reads back as a zero-valued `T` rather than as an absent key.

## Prefixes

Pass the prefix **without** a trailing colon — `"product"`, not `"product:"`.
`Key` inserts the separators, so a trailing colon yields `product::tenant:id`.

## Store

`Store` is the untyped string-valued backend: Redis in production, a no-op when
none is configured. It is what the FX module provides and what `Cache[T]` sits
on. Applications should not depend on it directly.

It previously carried an `interface{}`-based struct API (`GetStruct`,
`SetStruct`, `GetOrElseStruct`, `GetOrElseStructWithExpiry`). Those are gone:
`Cache[T]` does the serialization, with real types.
