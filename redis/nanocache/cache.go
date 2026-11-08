package nanocache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// Cache is a type-safe cache for one kind of value, and the type applications
// are expected to use.
//
// It owns a key namespace and the serialization: every key it builds is its
// prefix followed by the caller's parts, joined with ":". A part may be free
// text: its ":" and "\" are escaped, so each list of parts has one key and a
// part never acts as a separator. Callers address one entry through Key and
// chain from there:
//
//	products, err := nanocache.New[product.Product](store, "product", 30*time.Minute, logger)
//
//	prod, err := products.Key(tenantID, productID).GetOrElse(ctx, loadFromDB)
//	err = products.Key(tenantID, productID).Evict(ctx)
//	err = products.EvictPrefix(ctx, tenantID)
//
// Cache failures are treated as non-fatal and logged at warn: a cache that is
// down should degrade a read, not fail a request. Callers still receive the
// error and decide.
//
// It is a generic type rather than methods on Store because an interface
// method cannot declare type parameters.
type Cache[T any] struct {
	store  Store
	prefix string
	ttl    time.Duration
	logger *slog.Logger
}

// ErrInvalidPrefix is the error New returns for a prefix it cannot use.
var ErrInvalidPrefix = errors.New("nanocache: invalid prefix")

// New builds a Cache over store. Keys are namespaced under prefix, and writes
// expire after ttl. The prefix is a constant of the service and is used as
// given, so it may hold ":" for a hierarchy ("billing:product").
//
// New fails with ErrInvalidPrefix when prefix is empty, holds a glob
// character (* ? [ ] or \) or ends in ":". An empty prefix owns no
// namespace: every key starts with ":", and EvictPrefix of one cache reaches
// the keys of every other empty-prefix cache. A glob character would need
// escaping, and a trailing ":" gives an empty first part.
//
// Prefixes must not nest. Two caches whose prefixes are one the other
// followed by ":" ("billing" and "billing:product") share keys: Key("product",
// "p1") of the first is Key("p1") of the second, and EvictPrefix of the first
// drops the entries of the second. New cannot see the other caches, so it
// does not check this.
func New[T any](store Store, prefix string, ttl time.Duration, logger *slog.Logger) (Cache[T], error) {
	if prefix == "" {
		return Cache[T]{}, fmt.Errorf("%w: it is empty", ErrInvalidPrefix)
	}
	if i := strings.IndexAny(prefix, `*?[]\`); i >= 0 {
		return Cache[T]{}, fmt.Errorf("%w %q: %q is a glob character", ErrInvalidPrefix, prefix, prefix[i])
	}
	if strings.HasSuffix(prefix, ":") {
		return Cache[T]{}, fmt.Errorf("%w %q: it ends in \":\"", ErrInvalidPrefix, prefix)
	}
	return Cache[T]{
		store:  store,
		prefix: prefix,
		ttl:    ttl,
		logger: logger.With(slog.String(log.KeyComponent, "cache"), slog.String("cache_prefix", prefix)),
	}, nil
}

// Key addresses a single entry. Parts are joined with ":" under the prefix,
// each with ":" escaped as "\:" and "\" as "\\".
func (c Cache[T]) Key(parts ...string) Entry[T] {
	return Entry[T]{cache: c, key: c.buildKey(parts...)}
}

// EvictPrefix drops every entry nested under the given key parts — pass none
// for the whole namespace. Use it to invalidate a scope, such as all values for
// one tenant, without listing individual keys.
//
// It matches keys with at least one further part, so an entry addressed by the
// bare prefix (Key() with no parts) is not covered. The glob characters of the
// prefix and the parts are escaped, so a part such as "*" or "team[ops]"
// matches only itself.
func (c Cache[T]) EvictPrefix(ctx context.Context, parts ...string) error {
	pattern := globEscape(c.buildKey(parts...)) + ":*"
	if err := c.store.EvictPattern(ctx, pattern); err != nil {
		c.logger.WarnContext(ctx, "failed to evict cache pattern", log.Err(err), slog.String("pattern", pattern))
		return err
	}
	c.logger.DebugContext(ctx, "evicted cache pattern", slog.String("pattern", pattern))
	return nil
}

func (c Cache[T]) buildKey(parts ...string) string {
	var b strings.Builder
	b.WriteString(c.prefix)
	for _, part := range parts {
		b.WriteByte(':')
		_, _ = partEscaper.WriteString(&b, part) // A strings.Builder does not fail.
	}
	return b.String()
}

// partEscaper gives a key part one encoding: the escape character and the
// separator are escaped.
var partEscaper = strings.NewReplacer(`\`, `\\`, ":", `\:`) //nolint:gochecknoglobals // A stateless replacer.

// globEscaper escapes the characters of a Redis glob pattern.
var globEscaper = strings.NewReplacer( //nolint:gochecknoglobals // A stateless replacer.
	`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`,
)

// globEscape returns the pattern that matches s literally.
func globEscape(s string) string { return globEscaper.Replace(s) }

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
		e.cache.logger.WarnContext(ctx, "failed to read from cache", log.Err(err), slog.String("cache_key", e.key))
		return nil, err
	}
	return e.decode(ctx, raw)
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
	raw, err := encode(value)
	if err != nil {
		e.cache.logger.WarnContext(
			ctx,
			"failed to encode value for cache",
			log.Err(err),
			slog.String("cache_key", e.key),
		)
		return err
	}
	if setErr := e.cache.store.Set(ctx, e.key, raw, e.cache.ttl); setErr != nil {
		e.cache.logger.WarnContext(ctx, "failed to write to cache", log.Err(setErr), slog.String("cache_key", e.key))
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
// Signal absence only with a nil value: every error from load propagates,
// including ErrCacheKeyNotFound.
func (e Entry[T]) GetOrElse(ctx context.Context, load func() (*T, error)) (*T, error) {
	raw, err := e.cache.store.Get(ctx, e.key)
	switch {
	case err == nil:
		// A cached entry that will not decode is treated as unreadable rather
		// than fatal — a T whose shape changed between deploys would otherwise
		// poison the key until its TTL expired. Reloading overwrites it.
		if decoded, decodeErr := e.decode(ctx, raw); decodeErr == nil {
			return decoded, nil
		}
	case !errors.Is(err, ErrCacheKeyNotFound):
		// Read failure, not a miss. Fall through to load so the cache being
		// unavailable costs latency rather than the whole request.
		e.cache.logger.WarnContext(
			ctx,
			"failed to read from cache, loading directly",
			log.Err(err),
			slog.String("cache_key", e.key),
		)
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
	raw, err := encode(value)
	if err != nil {
		e.cache.logger.WarnContext(
			ctx,
			"failed to encode value for cache",
			log.Err(err),
			slog.String("cache_key", e.key),
		)
		return value
	}
	if setErr := e.cache.store.Set(ctx, e.key, raw, e.cache.ttl); setErr != nil {
		e.cache.logger.WarnContext(ctx, "failed to write to cache", log.Err(setErr), slog.String("cache_key", e.key))
		return value
	}
	decoded, decodeErr := e.decode(ctx, raw)
	if decodeErr != nil {
		return value
	}
	return decoded
}

// Evict drops this key.
func (e Entry[T]) Evict(ctx context.Context) error {
	if err := e.cache.store.Evict(ctx, e.key); err != nil {
		e.cache.logger.WarnContext(ctx, "failed to evict cache key", log.Err(err), slog.String("cache_key", e.key))
		return err
	}
	return nil
}

// Exists reports whether the key is currently present.
func (e Entry[T]) Exists(ctx context.Context) (bool, error) {
	found, err := e.cache.store.Exists(ctx, e.key)
	if err != nil {
		e.cache.logger.WarnContext(ctx, "failed to check cache key", log.Err(err), slog.String("cache_key", e.key))
		return false, err
	}
	return found, nil
}

func (e Entry[T]) decode(ctx context.Context, raw string) (*T, error) {
	var value T
	if err := decode(raw, &value); err != nil {
		e.cache.logger.WarnContext(ctx, "failed to decode cached value", log.Err(err), slog.String("cache_key", e.key))
		return nil, err
	}
	return &value, nil
}
