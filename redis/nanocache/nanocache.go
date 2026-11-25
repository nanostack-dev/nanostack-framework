// Package nanocache forwards to redis/nanoredis/cache, its new home.
//
// Deprecated: import github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache
// instead. Every name here is an alias of the same name there, so values,
// errors and the Store mix freely during the move. This package goes away
// after one release.
package nanocache

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
)

// Module provides Store, as cache.Module does.
var Module = nano.Module("cache", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewStore),
)

type (
	// Config is cache.Config.
	Config = cache.Config
	// Store is cache.Store.
	Store = cache.Store
	// RedisStore is cache.RedisStore.
	RedisStore = cache.RedisStore
	// Cache is cache.Cache.
	Cache[T any] = cache.Cache[T]
	// Entry is cache.Entry.
	Entry[T any] = cache.Entry[T]
)

var (
	// ErrCacheKeyNotFound is cache.ErrCacheKeyNotFound.
	ErrCacheKeyNotFound = cache.ErrCacheKeyNotFound
	// ErrInvalidPrefix is cache.ErrInvalidPrefix.
	ErrInvalidPrefix = cache.ErrInvalidPrefix
	// ErrNilValue is cache.ErrNilValue.
	ErrNilValue = cache.ErrNilValue
)

// NewStore is cache.NewStore.
func NewStore(ctx context.Context, cfg Config, logger *slog.Logger) Store {
	return cache.NewStore(ctx, cfg, logger)
}

// NewRedisStore is cache.NewRedisStore.
func NewRedisStore(client *redis.Client) *RedisStore { return cache.NewRedisStore(client) }

// NoOp is cache.NoOp.
func NoOp() Store { return cache.NoOp() }

// New is cache.New.
func New[T any](store Store, prefix string, ttl time.Duration, logger *slog.Logger) (Cache[T], error) {
	return cache.New[T](store, prefix, ttl, logger)
}
