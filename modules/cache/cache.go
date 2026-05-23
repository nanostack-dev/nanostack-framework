package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrCacheKeyNotFound = errors.New("cache key not found")

type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	GetOrElse(ctx context.Context, key string, fallback func() (string, error), expiration time.Duration) (string, error)
	GetOrElseWithExpiry(ctx context.Context, key string, fallback func() (string, time.Duration, error)) (string, error)
	GetStruct(ctx context.Context, key string, dest interface{}) error
	SetStruct(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	GetOrElseStruct(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, error), expiration time.Duration) error
	GetOrElseStructWithExpiry(ctx context.Context, key string, dest interface{}, fallback func() (interface{}, time.Duration, error)) error
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

func SerializeStruct(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func DeserializeStruct(data string, dest interface{}) error {
	return json.Unmarshal([]byte(data), dest)
}
