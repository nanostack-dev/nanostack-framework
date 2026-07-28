package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type NoOpCache struct{}

func NewNoOpCache() *NoOpCache { return &NoOpCache{} }

func (n *NoOpCache) Get(_ context.Context, _ string) (string, error) { return "", ErrCacheKeyNotFound }

func (n *NoOpCache) Set(_ context.Context, _ string, _ string, _ time.Duration) error { return nil }

func (n *NoOpCache) GetOrElse(_ context.Context, _ string, fallback func() (string, error), _ time.Duration) (string, error) {
	return fallback()
}

func (n *NoOpCache) GetOrElseWithExpiry(_ context.Context, _ string, fallback func() (string, time.Duration, error)) (string, error) {
	value, _, err := fallback()
	return value, err
}

func (n *NoOpCache) Evict(_ context.Context, _ string) error { return nil }

func (n *NoOpCache) EvictPattern(_ context.Context, _ string) error { return nil }

func (n *NoOpCache) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

func (n *NoOpCache) RedisClient() *redis.Client { return nil }

func (n *NoOpCache) Close() error { return nil }
