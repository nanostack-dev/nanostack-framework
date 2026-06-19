package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// redisPingTimeout bounds the startup connectivity probe so an unreachable Redis
// fails fast (and the caller can fall back to a no-op cache) instead of hanging.
const redisPingTimeout = 5 * time.Second

type RedisCache struct {
	client *redis.Client
}

// NewRedisCache connects to Redis and verifies the connection with a bounded
// ping. It returns an error (rather than panicking) when Redis is unreachable so
// callers can degrade to a no-op cache: caching is only active on a live Redis.
func NewRedisCache(config Config, logger zerolog.Logger) (*RedisCache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: config.Address, Password: config.Password, DB: config.DB})

	ctx, cancel := context.WithTimeout(context.Background(), redisPingTimeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn().Err(err).Str("address", config.Address).Msg("redis cache not reachable")
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to redis cache at %q: %w", config.Address, err)
	}
	return &RedisCache{client: rdb}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	value, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheKeyNotFound
	}
	return value, err
}

func (r *RedisCache) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *RedisCache) GetOrElse(ctx context.Context, key string, fallback func() (string, error), expiration time.Duration) (string, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", err
	}
	value, err = fallback()
	if err != nil {
		return "", err
	}
	if setErr := r.client.Set(ctx, key, value, expiration).Err(); setErr != nil {
		return value, setErr
	}
	return value, nil
}

func (r *RedisCache) GetOrElseWithExpiry(ctx context.Context, key string, fallback func() (string, time.Duration, error)) (string, error) {
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

func (r *RedisCache) Evict(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	count, err := r.client.Exists(ctx, key).Result()
	return count > 0, err
}

func (r *RedisCache) RedisClient() *redis.Client { return r.client }

func (r *RedisCache) EvictPattern(ctx context.Context, pattern string) error {
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil || len(keys) == 0 {
		return err
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisCache) GetStruct(ctx context.Context, key string, dest interface{}) error {
	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrCacheKeyNotFound
		}
		return err
	}
	return DeserializeStruct(value, dest)
}

func (r *RedisCache) SetStruct(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	serialized, err := SerializeStruct(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, serialized, expiration).Err()
}

func (r *RedisCache) GetOrElseStruct(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, error), expiration time.Duration) error {
	if err := r.GetStruct(ctx, key, dest); err == nil {
		return nil
	} else if !errors.Is(err, ErrCacheKeyNotFound) {
		return err
	}
	value, err := fallback()
	if err != nil || value == nil {
		return err
	}
	if err := r.SetStruct(ctx, key, value, expiration); err != nil {
		return err
	}
	serialized, err := SerializeStruct(value)
	if err != nil {
		return err
	}
	return DeserializeStruct(serialized, dest)
}

func (r *RedisCache) GetOrElseStructWithExpiry(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, time.Duration, error)) error {
	if err := r.GetStruct(ctx, key, dest); err == nil {
		return nil
	} else if !errors.Is(err, ErrCacheKeyNotFound) {
		return err
	}
	value, expiration, err := fallback()
	if err != nil {
		return err
	}
	if err := r.SetStruct(ctx, key, value, expiration); err != nil {
		return err
	}
	serialized, err := SerializeStruct(value)
	if err != nil {
		return err
	}
	return DeserializeStruct(serialized, dest)
}

func (r *RedisCache) Close() error { return r.client.Close() }
