package nanocache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// pingTimeout bounds the connection check of NewStore, so an unreachable
// Redis falls back to the no-op store quickly instead of holding the start.
const pingTimeout = 5 * time.Second

// ErrCacheKeyNotFound is returned when a key is absent.
var ErrCacheKeyNotFound = errors.New("cache key not found")

// Module provides Store.
var Module = nano.Module("cache", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewStore),
)

// Config is the Redis connection. Every field is optional: an empty Address
// selects the no-op store.
type Config struct {
	// Address is host:port of the Redis server.
	Address string `config:"address,optional" json:"address" yaml:"address"`
	// Password authenticates to the server.
	Password secret.Secret `config:"password,optional" json:"password" yaml:"password"`
	// DB is the Redis logical database.
	DB int `config:"db,optional" validate:"min=0" json:"db" yaml:"db"`
}

// Store is the untyped string-valued backend behind a cache. Services use
// Cache[T], which owns a key namespace and the encoding; Store exists so
// Cache[T] has something to sit on and the module has one thing to provide.
//
// Its methods are those of modules/cache.Store, so one value satisfies both
// interfaces while a service moves from fx.
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	GetOrElse(
		ctx context.Context, key string, fallback func() (string, error), expiration time.Duration,
	) (string, error)
	GetOrElseWithExpiry(
		ctx context.Context, key string, fallback func() (string, time.Duration, error),
	) (string, error)
	Evict(ctx context.Context, key string) error
	EvictPattern(ctx context.Context, pattern string) error
	Exists(ctx context.Context, key string) (bool, error)
	// RedisClient returns the client, or nil for the no-op store.
	RedisClient() *redis.Client
	Close() error
}

// NewStore returns the Redis store when cfg.Address is set and the server
// answers a ping, and the no-op store otherwise. The Redis store is a nano
// Stopper, which closes the client, and a HealthChecker, which pings it.
func NewStore(ctx context.Context, cfg Config, logger *slog.Logger) Store {
	logger = logger.With(slog.String(log.KeyComponent, "cache"))
	if cfg.Address == "" {
		logger.WarnContext(ctx, "cache address empty; using no-op cache")
		return NoOp()
	}
	store, err := dial(ctx, cfg)
	if err != nil {
		logger.WarnContext(ctx, "redis cache not connected; using no-op cache", log.Err(err))
		return NoOp()
	}
	logger.InfoContext(ctx, "redis cache connected", slog.String("address", cfg.Address))
	return store
}

func dial(ctx context.Context, cfg Config) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{Addr: cfg.Address, Password: cfg.Password.Reveal(), DB: cfg.DB})
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("nanocache: connect to %q: %w", cfg.Address, err)
	}
	return &RedisStore{client: client}, nil
}

// RedisStore is the Store over a Redis client.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore returns the Store over client, for a service that owns its
// client. NewStore builds its own.
func NewRedisStore(client *redis.Client) *RedisStore { return &RedisStore{client: client} }

// Get returns the value, or ErrCacheKeyNotFound.
func (r *RedisStore) Get(ctx context.Context, key string) (string, error) {
	value, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheKeyNotFound
	}
	return value, err
}

// Set writes value under key, expiring after expiration (0: never).
func (r *RedisStore) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

// GetOrElse returns the value, or calls fallback on a miss and writes its
// result with expiration.
func (r *RedisStore) GetOrElse(
	ctx context.Context, key string, fallback func() (string, error), expiration time.Duration,
) (string, error) {
	return r.GetOrElseWithExpiry(ctx, key, func() (string, time.Duration, error) {
		value, err := fallback()
		return value, expiration, err
	})
}

// GetOrElseWithExpiry is GetOrElse where fallback also chooses the
// expiration. A write failure returns the loaded value with the error.
func (r *RedisStore) GetOrElseWithExpiry(
	ctx context.Context, key string, fallback func() (string, time.Duration, error),
) (string, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", err
	}
	value, expiration, err := fallback()
	if err != nil {
		return "", err
	}
	if setErr := r.client.Set(ctx, key, value, expiration).Err(); setErr != nil {
		return value, setErr
	}
	return value, nil
}

// Evict deletes key.
func (r *RedisStore) Evict(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// EvictPattern deletes every key that matches the Redis glob pattern. It
// walks the keyspace with SCAN, so it does not block the server as KEYS does.
func (r *RedisStore) EvictPattern(ctx context.Context, pattern string) error {
	iter := r.client.Scan(ctx, 0, pattern, scanCount).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil || len(keys) == 0 {
		return err
	}
	return r.client.Del(ctx, keys...).Err()
}

// scanCount is the SCAN batch hint of EvictPattern.
const scanCount = 500

// Exists reports whether key is present.
func (r *RedisStore) Exists(ctx context.Context, key string) (bool, error) {
	count, err := r.client.Exists(ctx, key).Result()
	return count > 0, err
}

// RedisClient returns the client.
func (r *RedisStore) RedisClient() *redis.Client { return r.client }

// Close closes the client.
func (r *RedisStore) Close() error { return r.client.Close() }

// Stop closes the client when the App stops.
func (r *RedisStore) Stop(context.Context) error { return r.client.Close() }

// CheckHealth pings the server, for /readyz.
func (r *RedisStore) CheckHealth(ctx context.Context) error { return r.client.Ping(ctx).Err() }

// noopStore stores nothing: every read misses and every fallback runs.
type noopStore struct{}

// NoOp returns the Store that stores nothing.
func NoOp() Store { return noopStore{} }

func (noopStore) Get(context.Context, string) (string, error) { return "", ErrCacheKeyNotFound }

func (noopStore) Set(context.Context, string, string, time.Duration) error { return nil }

func (noopStore) GetOrElse(
	_ context.Context,
	_ string,
	fallback func() (string, error),
	_ time.Duration,
) (string, error) {
	return fallback()
}

func (noopStore) GetOrElseWithExpiry(
	_ context.Context, _ string, fallback func() (string, time.Duration, error),
) (string, error) {
	value, _, err := fallback()
	return value, err
}

func (noopStore) Evict(context.Context, string) error { return nil }

func (noopStore) EvictPattern(context.Context, string) error { return nil }

func (noopStore) Exists(context.Context, string) (bool, error) { return false, nil }

func (noopStore) RedisClient() *redis.Client { return nil }

func (noopStore) Close() error { return nil }

// encode and decode are the JSON encoding of Cache[T].
func encode(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decode(data string, dest any) error {
	return json.Unmarshal([]byte(data), dest)
}
