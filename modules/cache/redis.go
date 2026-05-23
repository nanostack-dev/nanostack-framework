package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(config Config, logger zerolog.Logger) *RedisCache {
	rdb := redis.NewClient(&redis.Options{Addr: config.Address, Password: config.Password, DB: config.DB})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logger.Error().Err(err).Msg("failed to connect to redis cache")
		panic("failed to connect to redis cache: " + err.Error())
	}
	return &RedisCache{client: rdb}
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
