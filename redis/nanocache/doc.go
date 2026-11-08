// Package nanocache is the nano module for the shared cache: a Redis store
// when an address is configured, a no-op store otherwise, and the typed
// Cache[T] that services use on top of it.
//
// It replaces the deprecated fx module modules/cache. A service lists Module
// in its manifest and puts Config in its own Config under the section it
// chooses. Echopoint chooses redis, which keeps REDIS_ADDRESS, REDIS_PASSWORD
// and REDIS_DB:
//
//	type Config struct {
//		Redis nanocache.Config `config:"redis"`
//	}
//
// A cache is an optimization, never a dependency: when Redis is not
// configured or not reachable at start, NewStore returns the no-op store and
// every read goes to the source of truth. A per-instance fallback cache would
// keep stale entries that no other instance can evict.
package nanocache
