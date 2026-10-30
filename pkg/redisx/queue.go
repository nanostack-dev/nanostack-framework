package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/redis/go-redis/v9"
)

// Queue is a first-in, first-out Redis list of typed items under one key.
//
// A popped item is gone: if the process that popped it stops before it handles
// it, the item is lost. Use a database-backed queue for work that must not be
// lost.
type Queue[T any] struct {
	rdb   redis.UniversalClient
	key   string
	codec Codec[T]
	ttl   time.Duration
}

// Queue returns the queue stored under name.
func (c *Client) Queue[T any](name string) Queue[T] {
	return Queue[T]{rdb: c.rdb, key: c.Key(name), codec: defaultCodec[T]()}
}

// WithTTL makes every push move the expiry of the whole queue to ttl from now.
func (q Queue[T]) WithTTL(ttl time.Duration) Queue[T] {
	q.ttl = ttl
	return q
}

// WithCodec replaces the default codec.
func (q Queue[T]) WithCodec(codec Codec[T]) Queue[T] {
	q.codec = codec
	return q
}

// Key returns the full key name.
func (q Queue[T]) Key() string { return q.key }

// Push adds items at the back and returns the new length.
func (q Queue[T]) Push(ctx context.Context, items ...T) (int, error) {
	if len(items) == 0 {
		return q.Len(ctx)
	}
	encoded, err := encodeAll(q.codec, items)
	if err != nil {
		return 0, err
	}
	var length *redis.IntCmd
	_, err = q.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		length = pipe.RPush(ctx, q.key, encoded...)
		if q.ttl > 0 {
			pipe.PExpire(ctx, q.key, q.ttl)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("redisx: push to %s: %w", q.key, err)
	}
	return int(length.Val()), nil
}

// Pop removes and returns the front item. It is absent when the queue is
// empty.
func (q Queue[T]) Pop(ctx context.Context) (functional.Option[T], error) {
	raw, err := q.rdb.LPop(ctx, q.key).Result()
	return q.decodeOptional(raw, err)
}

// PopWait is Pop that waits up to timeout for an item. Redis counts the wait in
// whole seconds, so timeout is rounded down, with a minimum of one second. The
// result is absent when no item came in time.
func (q Queue[T]) PopWait(ctx context.Context, timeout time.Duration) (functional.Option[T], error) {
	timeout = max(timeout.Truncate(time.Second), time.Second)
	reply, err := q.rdb.BLPop(ctx, timeout, q.key).Result()
	if err != nil || len(reply) < 2 {
		return q.decodeOptional("", err)
	}
	return q.decodeOptional(reply[1], nil)
}

// Peek returns the front item without removing it.
func (q Queue[T]) Peek(ctx context.Context) (functional.Option[T], error) {
	raw, err := q.rdb.LIndex(ctx, q.key, 0).Result()
	return q.decodeOptional(raw, err)
}

// Len returns the number of items.
func (q Queue[T]) Len(ctx context.Context) (int, error) {
	length, err := q.rdb.LLen(ctx, q.key).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: len %s: %w", q.key, err)
	}
	return int(length), nil
}

// Clear removes every item.
func (q Queue[T]) Clear(ctx context.Context) error {
	if err := q.rdb.Del(ctx, q.key).Err(); err != nil {
		return fmt.Errorf("redisx: clear %s: %w", q.key, err)
	}
	return nil
}

func (q Queue[T]) decodeOptional(raw string, err error) (functional.Option[T], error) {
	if errors.Is(err, redis.Nil) {
		return functional.None[T](), nil
	}
	if err != nil {
		return functional.None[T](), fmt.Errorf("redisx: pop %s: %w", q.key, err)
	}
	value, err := q.codec.Decode(raw)
	if err != nil {
		return functional.None[T](), err
	}
	return functional.Some(value), nil
}
