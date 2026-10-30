package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/redis/go-redis/v9"
)

const maxUpdateAttempts = 32

// Value is one typed value stored under one key.
type Value[T any] struct {
	rdb   redis.UniversalClient
	key   string
	codec Codec[T]
	ttl   time.Duration
}

// Value returns the value stored under name. Nothing is read or written until
// you call a method.
func (c *Client) Value[T any](name string) Value[T] {
	return Value[T]{rdb: c.rdb, key: c.Key(name), codec: defaultCodec[T]()}
}

// WithTTL makes every write expire the value after ttl. Zero, the default,
// keeps it until it is deleted.
func (v Value[T]) WithTTL(ttl time.Duration) Value[T] {
	v.ttl = ttl
	return v
}

// WithCodec replaces the default codec.
func (v Value[T]) WithCodec(codec Codec[T]) Value[T] {
	v.codec = codec
	return v
}

// Key returns the full key name.
func (v Value[T]) Key() string { return v.key }

// Get reads the value. It is absent when the key does not exist.
func (v Value[T]) Get(ctx context.Context) (functional.Option[T], error) {
	raw, err := v.rdb.Get(ctx, v.key).Result()
	return v.decodeOptional(raw, err)
}

// Set writes the value.
func (v Value[T]) Set(ctx context.Context, value T) error {
	raw, err := v.codec.Encode(value)
	if err != nil {
		return err
	}
	if err := v.rdb.Set(ctx, v.key, raw, v.ttl).Err(); err != nil {
		return fmt.Errorf("redisx: set %s: %w", v.key, err)
	}
	return nil
}

// SetIfAbsent writes the value only if the key does not exist, and reports
// whether it did.
func (v Value[T]) SetIfAbsent(ctx context.Context, value T) (bool, error) {
	raw, err := v.codec.Encode(value)
	if err != nil {
		return false, err
	}
	written, err := v.rdb.SetNX(ctx, v.key, raw, v.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redisx: set %s if absent: %w", v.key, err)
	}
	return written, nil
}

// Swap writes the value and returns the one it replaced, in one step.
func (v Value[T]) Swap(ctx context.Context, value T) (functional.Option[T], error) {
	raw, err := v.codec.Encode(value)
	if err != nil {
		return functional.None[T](), err
	}
	previous, err := v.rdb.SetArgs(ctx, v.key, raw, redis.SetArgs{Get: true, TTL: v.ttl}).Result()
	return v.decodeOptional(previous, err)
}

// Delete removes the value and reports whether it existed.
func (v Value[T]) Delete(ctx context.Context) (bool, error) {
	removed, err := v.rdb.Del(ctx, v.key).Result()
	if err != nil {
		return false, fmt.Errorf("redisx: delete %s: %w", v.key, err)
	}
	return removed > 0, nil
}

// Exists reports whether the key exists.
func (v Value[T]) Exists(ctx context.Context) (bool, error) {
	count, err := v.rdb.Exists(ctx, v.key).Result()
	if err != nil {
		return false, fmt.Errorf("redisx: exists %s: %w", v.key, err)
	}
	return count > 0, nil
}

// Update reads the value, gives it to fn and writes what fn returns, as one
// atomic step: if another writer changes the value in between, Update runs fn
// again on the new value. fn must not have side effects. Without WithTTL, the
// value keeps the expiry it had. Update returns ErrConflict when the value
// changed during every attempt, and fn's error unchanged.
func (v Value[T]) Update(ctx context.Context, fn func(current functional.Option[T]) (T, error)) (T, error) {
	var next T
	var fnErr error
	attempt := func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, v.key).Result()
		current, err := v.decodeOptional(raw, err)
		if err != nil {
			return err
		}
		next, fnErr = fn(current)
		if fnErr != nil {
			return fnErr
		}
		encoded, err := v.codec.Encode(next)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			if v.ttl > 0 {
				pipe.Set(ctx, v.key, encoded, v.ttl)
			} else {
				pipe.SetArgs(ctx, v.key, encoded, redis.SetArgs{KeepTTL: true})
			}
			return nil
		})
		return err
	}
	for range maxUpdateAttempts {
		err := v.rdb.Watch(ctx, attempt, v.key)
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		var zero T
		if fnErr != nil {
			return zero, fnErr
		}
		if err != nil {
			return zero, fmt.Errorf("redisx: update %s: %w", v.key, err)
		}
		return next, nil
	}
	var zero T
	return zero, fmt.Errorf("redisx: update %s: %w", v.key, ErrConflict)
}

func (v Value[T]) decodeOptional(raw string, err error) (functional.Option[T], error) {
	if errors.Is(err, redis.Nil) {
		return functional.None[T](), nil
	}
	if err != nil {
		return functional.None[T](), fmt.Errorf("redisx: get %s: %w", v.key, err)
	}
	value, err := v.codec.Decode(raw)
	if err != nil {
		return functional.None[T](), err
	}
	return functional.Some(value), nil
}
