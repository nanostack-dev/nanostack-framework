package cache

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Cache is a type-safe cache for one kind of value, and the type applications
// are expected to use.
//
// It owns a key namespace and the serialization: every key it builds is its
// prefix followed by the caller's parts, joined with ":". Callers address one
// entry through Key and chain from there:
//
//	products := cache.New[product.Product](store, "product", 30*time.Minute, logger)
//
//	prod, err := products.Key(tenantID, productID).GetOrElse(ctx, loadFromDB)
//	err = products.Key(tenantID, productID).Evict(ctx)
//	err = products.EvictPrefix(ctx, tenantID)
//
// Cache failures are treated as non-fatal and logged at warn: a cache that is
// down should degrade a read, not fail a request. Callers still receive the
// error and decide.
//
// It is a generic type rather than methods on Store because Go methods cannot
// declare their own type parameters.
type Cache[T any] struct {
	store  Store
	prefix string
	ttl    time.Duration
	logger zerolog.Logger
}

// New builds a Cache over store. Keys are namespaced under prefix, and writes
// expire after ttl.
func New[T any](store Store, prefix string, ttl time.Duration, logger zerolog.Logger) Cache[T] {
	return Cache[T]{
		store:  store,
		prefix: prefix,
		ttl:    ttl,
		logger: logger.With().Str("component", "cache").Str("cache_prefix", prefix).Logger(),
	}
}

// Key addresses a single entry. Parts are joined with ":" under the prefix.
func (c Cache[T]) Key(parts ...string) Entry[T] {
	return Entry[T]{cache: c, key: c.buildKey(parts...)}
}

// EvictPrefix drops every entry nested under the given key parts — pass none
// for the whole namespace. Use it to invalidate a scope, such as all values for
// one tenant, without listing individual keys.
//
// It matches keys with at least one further part, so an entry addressed by the
// bare prefix (Key() with no parts) is not covered.
func (c Cache[T]) EvictPrefix(ctx context.Context, parts ...string) error {
	pattern := c.buildKey(parts...) + ":*"
	if err := c.store.EvictPattern(ctx, pattern); err != nil {
		c.logger.Warn().Err(err).Str("pattern", pattern).Msg("failed to evict cache pattern")
		return err
	}
	c.logger.Debug().Str("pattern", pattern).Msg("evicted cache pattern")
	return nil
}

func (c Cache[T]) buildKey(parts ...string) string {
	return strings.Join(append([]string{c.prefix}, parts...), ":")
}

// Entry is one addressed key within a Cache namespace.
type Entry[T any] struct {
	cache Cache[T]
	key   string
}

// String returns the fully qualified cache key, for logging and tests.
func (e Entry[T]) String() string { return e.key }

// Get returns the cached value, or ErrCacheKeyNotFound when the key is absent.
func (e Entry[T]) Get(ctx context.Context) (*T, error) {
	raw, err := e.cache.store.Get(ctx, e.key)
	if err != nil {
		if errors.Is(err, ErrCacheKeyNotFound) {
			return nil, ErrCacheKeyNotFound
		}
		e.warn(err, "failed to read from cache")
		return nil, err
	}
	return e.decode(raw)
}

// ErrNilValue is returned when a nil value is written. Storing it would encode
// JSON null, which reads back as a zero-valued T rather than as an absent key.
var ErrNilValue = errors.New("cache: refusing to store a nil value")

// Set writes value under this key with the namespace's TTL.
//
// A nil value is rejected: absence is expressed by not caching, matching
// GetOrElse's contract, not by storing a null.
func (e Entry[T]) Set(ctx context.Context, value *T) error {
	if value == nil {
		return ErrNilValue
	}
	raw, err := SerializeStruct(value)
	if err != nil {
		e.warn(err, "failed to encode value for cache")
		return err
	}
	if setErr := e.cache.store.Set(ctx, e.key, raw, e.cache.ttl); setErr != nil {
		e.warn(setErr, "failed to write to cache")
		return setErr
	}
	return nil
}

// GetOrElse returns the cached value, calling load on a miss and caching what
// it returns.
//
// It degrades rather than fails. If the cache cannot be read, load is called
// anyway and its value returned; if the cache cannot be written, the loaded
// value is still returned. Either way the failure is logged at warn. A cache
// that is down should slow a request, not break it.
//
// A nil value from load means "does not exist": nothing is cached and
// GetOrElse returns (nil, nil), so an absent record stays distinguishable from
// a failure without forcing every caller to compare against
// ErrCacheKeyNotFound.
//
// load should not return ErrCacheKeyNotFound to mean anything other than
// absence — signal absence with a nil value instead.
func (e Entry[T]) GetOrElse(ctx context.Context, load func() (*T, error)) (*T, error) {
	raw, err := e.cache.store.Get(ctx, e.key)
	switch {
	case err == nil:
		return e.decode(raw)
	case !errors.Is(err, ErrCacheKeyNotFound):
		// Read failure, not a miss. Fall through to load so the cache being
		// unavailable costs latency rather than the whole request.
		e.warn(err, "failed to read from cache, loading directly")
	}

	loaded, err := load()
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		//nolint:nilnil // absence is not a failure; see the doc comment above
		return nil, nil
	}
	return e.store(ctx, loaded), nil
}

// store writes value to the cache and returns what callers should see. A write
// failure is logged, never returned: the value was loaded successfully, so the
// read succeeds regardless.
//
// The stored encoding is decoded back so a miss and a hit return the same
// representation; if that round trip fails, the loaded value is returned as-is.
func (e Entry[T]) store(ctx context.Context, value *T) *T {
	raw, err := SerializeStruct(value)
	if err != nil {
		e.warn(err, "failed to encode value for cache")
		return value
	}
	if setErr := e.cache.store.Set(ctx, e.key, raw, e.cache.ttl); setErr != nil {
		e.warn(setErr, "failed to write to cache")
		return value
	}
	decoded, decodeErr := e.decode(raw)
	if decodeErr != nil {
		return value
	}
	return decoded
}

// Evict drops this key.
func (e Entry[T]) Evict(ctx context.Context) error {
	if err := e.cache.store.Evict(ctx, e.key); err != nil {
		e.warn(err, "failed to evict cache key")
		return err
	}
	return nil
}

// Exists reports whether the key is currently present.
func (e Entry[T]) Exists(ctx context.Context) (bool, error) {
	found, err := e.cache.store.Exists(ctx, e.key)
	if err != nil {
		e.warn(err, "failed to check cache key")
		return false, err
	}
	return found, nil
}

func (e Entry[T]) decode(raw string) (*T, error) {
	var value T
	if err := DeserializeStruct(raw, &value); err != nil {
		e.warn(err, "failed to decode cached value")
		return nil, err
	}
	return &value, nil
}

func (e Entry[T]) warn(err error, msg string) {
	e.cache.logger.Warn().Err(err).Str("cache_key", e.key).Msg(msg)
}
