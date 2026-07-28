package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrCacheKeyNotFound is returned when a key is absent.
var ErrCacheKeyNotFound = errors.New("cache key not found")

// Store is the untyped string-valued backend behind a cache: Redis in
// production, a no-op when none is configured.
//
// Applications should not depend on Store directly — use Cache[T], which owns a
// key namespace and serialization. Store exists so Cache[T] has something to sit
// on, and so the FX module has one thing to provide.
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
	RedisClient() *redis.Client
	Close() error
}

type Config struct {
	Address  string `yaml:"address"  optional:"true"`
	Password string `yaml:"password" optional:"true"`
	DB       int    `yaml:"db"       optional:"true"`
}

// SerializeStruct encodes a value for storage.
func SerializeStruct(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeserializeStruct decodes a stored value.
func DeserializeStruct(data string, dest any) error {
	return json.Unmarshal([]byte(data), dest)
}
