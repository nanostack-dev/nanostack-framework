package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/redis/go-redis/v9"
)

// Map is a Redis hash: string fields with typed values, under one key.
type Map[V any] struct {
	rdb   redis.UniversalClient
	key   string
	codec Codec[V]
	ttl   time.Duration
}

// Map returns the hash stored under name.
func (c *Client) Map[V any](name string) Map[V] {
	return Map[V]{rdb: c.rdb, key: c.Key(name), codec: defaultCodec[V]()}
}

// WithTTL makes every write push the expiry of the whole map to ttl from now.
func (m Map[V]) WithTTL(ttl time.Duration) Map[V] {
	m.ttl = ttl
	return m
}

// WithCodec replaces the default codec.
func (m Map[V]) WithCodec(codec Codec[V]) Map[V] {
	m.codec = codec
	return m
}

// Key returns the full key name.
func (m Map[V]) Key() string { return m.key }

// Get reads one field. It is absent when the field does not exist.
func (m Map[V]) Get(ctx context.Context, field string) (functional.Option[V], error) {
	raw, err := m.rdb.HGet(ctx, m.key, field).Result()
	if errors.Is(err, redis.Nil) {
		return functional.None[V](), nil
	}
	if err != nil {
		return functional.None[V](), fmt.Errorf("redisx: get %s[%s]: %w", m.key, field, err)
	}
	value, err := m.codec.Decode(raw)
	if err != nil {
		return functional.None[V](), err
	}
	return functional.Some(value), nil
}

// Set writes one field.
func (m Map[V]) Set(ctx context.Context, field string, value V) error {
	return m.SetAll(ctx, map[string]V{field: value})
}

// SetAll writes several fields in one step.
func (m Map[V]) SetAll(ctx context.Context, values map[string]V) error {
	if len(values) == 0 {
		return nil
	}
	pairs := make([]any, 0, 2*len(values))
	for field, value := range values {
		raw, err := m.codec.Encode(value)
		if err != nil {
			return err
		}
		pairs = append(pairs, field, raw)
	}
	_, err := m.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, m.key, pairs...)
		m.expire(ctx, pipe)
		return nil
	})
	if err != nil {
		return fmt.Errorf("redisx: set %s: %w", m.key, err)
	}
	return nil
}

// SetIfAbsent writes one field only if it does not exist, and reports whether
// it did.
func (m Map[V]) SetIfAbsent(ctx context.Context, field string, value V) (bool, error) {
	raw, err := m.codec.Encode(value)
	if err != nil {
		return false, err
	}
	var written *redis.BoolCmd
	_, err = m.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		written = pipe.HSetNX(ctx, m.key, field, raw)
		m.expire(ctx, pipe)
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("redisx: set %s[%s] if absent: %w", m.key, field, err)
	}
	return written.Val(), nil
}

// Delete removes fields and returns how many existed.
func (m Map[V]) Delete(ctx context.Context, fields ...string) (int, error) {
	if len(fields) == 0 {
		return 0, nil
	}
	removed, err := m.rdb.HDel(ctx, m.key, fields...).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: delete from %s: %w", m.key, err)
	}
	return int(removed), nil
}

// Has reports whether a field exists.
func (m Map[V]) Has(ctx context.Context, field string) (bool, error) {
	found, err := m.rdb.HExists(ctx, m.key, field).Result()
	if err != nil {
		return false, fmt.Errorf("redisx: has %s[%s]: %w", m.key, field, err)
	}
	return found, nil
}

// Len returns the number of fields.
func (m Map[V]) Len(ctx context.Context) (int, error) {
	count, err := m.rdb.HLen(ctx, m.key).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: len %s: %w", m.key, err)
	}
	return int(count), nil
}

// All reads every field. Use it on maps you know are small.
func (m Map[V]) All(ctx context.Context) (map[string]V, error) {
	raws, err := m.rdb.HGetAll(ctx, m.key).Result()
	if err != nil {
		return nil, fmt.Errorf("redisx: read %s: %w", m.key, err)
	}
	values := make(map[string]V, len(raws))
	for field, raw := range raws {
		value, err := m.codec.Decode(raw)
		if err != nil {
			return nil, err
		}
		values[field] = value
	}
	return values, nil
}

// Clear removes the whole map.
func (m Map[V]) Clear(ctx context.Context) error {
	if err := m.rdb.Del(ctx, m.key).Err(); err != nil {
		return fmt.Errorf("redisx: clear %s: %w", m.key, err)
	}
	return nil
}

func (m Map[V]) expire(ctx context.Context, pipe redis.Pipeliner) {
	if m.ttl > 0 {
		pipe.PExpire(ctx, m.key, m.ttl)
	}
}
