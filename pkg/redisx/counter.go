package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Counter is an atomic 64-bit integer under one key. A missing key counts as
// zero.
type Counter struct {
	rdb redis.UniversalClient
	key string
	ttl time.Duration
}

// Counter returns the counter stored under name.
func (c *Client) Counter(name string) Counter {
	return Counter{rdb: c.rdb, key: c.Key(name)}
}

// WithTTL makes every change move the expiry of the counter to ttl from now.
func (c Counter) WithTTL(ttl time.Duration) Counter {
	c.ttl = ttl
	return c
}

// Key returns the full key name.
func (c Counter) Key() string { return c.key }

// Add adds delta, which can be negative, and returns the new count.
func (c Counter) Add(ctx context.Context, delta int64) (int64, error) {
	var count *redis.IntCmd
	_, err := c.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		count = pipe.IncrBy(ctx, c.key, delta)
		if c.ttl > 0 {
			pipe.PExpire(ctx, c.key, c.ttl)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("redisx: add to %s: %w", c.key, err)
	}
	return count.Val(), nil
}

// Incr adds one and returns the new count.
func (c Counter) Incr(ctx context.Context) (int64, error) { return c.Add(ctx, 1) }

// Get returns the count.
func (c Counter) Get(ctx context.Context) (int64, error) {
	count, err := c.rdb.Get(ctx, c.key).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("redisx: get %s: %w", c.key, err)
	}
	return count, nil
}

// Reset sets the count back to zero.
func (c Counter) Reset(ctx context.Context) error {
	if err := c.rdb.Del(ctx, c.key).Err(); err != nil {
		return fmt.Errorf("redisx: reset %s: %w", c.key, err)
	}
	return nil
}
