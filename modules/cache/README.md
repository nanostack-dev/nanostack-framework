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

Cache failures are non-fatal and logged at warn: a cache that is down should
degrade a read, not fail a request. The error still reaches the caller.

`GetOrElse` treats a nil from the loader as "does not exist" — nothing is
cached, and `(nil, nil)` comes back — so an absent record stays distinguishable
from a failure.

## Store

`Store` is the untyped string-valued backend: Redis in production, a no-op when
none is configured. It is what the FX module provides and what `Cache[T]` sits
on. Applications should not depend on it directly.

It previously carried an `interface{}`-based struct API (`GetStruct`,
`SetStruct`, `GetOrElseStruct`, `GetOrElseStructWithExpiry`). Those are gone:
`Cache[T]` does the serialization, with real types.
