package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Set is a Redis set of typed members under one key. Members are compared by
// their encoded form, so the codec must encode equal values identically.
type Set[T any] struct {
	rdb   redis.UniversalClient
	key   string
	codec Codec[T]
	ttl   time.Duration
}

// Set returns the set stored under name.
func (c *Client) Set[T any](name string) Set[T] {
	return Set[T]{rdb: c.rdb, key: c.Key(name), codec: defaultCodec[T]()}
}

// WithTTL makes every write push the expiry of the whole set to ttl from now.
func (s Set[T]) WithTTL(ttl time.Duration) Set[T] {
	s.ttl = ttl
	return s
}

// WithCodec replaces the default codec.
func (s Set[T]) WithCodec(codec Codec[T]) Set[T] {
	s.codec = codec
	return s
}

// Key returns the full key name.
func (s Set[T]) Key() string { return s.key }

// Add adds members and returns how many were new.
func (s Set[T]) Add(ctx context.Context, members ...T) (int, error) {
	if len(members) == 0 {
		return 0, nil
	}
	encoded, err := encodeAll(s.codec, members)
	if err != nil {
		return 0, err
	}
	var added *redis.IntCmd
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		added = pipe.SAdd(ctx, s.key, encoded...)
		if s.ttl > 0 {
			pipe.PExpire(ctx, s.key, s.ttl)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("redisx: add to %s: %w", s.key, err)
	}
	return int(added.Val()), nil
}

// Remove removes members and returns how many were present.
func (s Set[T]) Remove(ctx context.Context, members ...T) (int, error) {
	if len(members) == 0 {
		return 0, nil
	}
	encoded, err := encodeAll(s.codec, members)
	if err != nil {
		return 0, err
	}
	removed, err := s.rdb.SRem(ctx, s.key, encoded...).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: remove from %s: %w", s.key, err)
	}
	return int(removed), nil
}

// Contains reports whether member is in the set.
func (s Set[T]) Contains(ctx context.Context, member T) (bool, error) {
	raw, err := s.codec.Encode(member)
	if err != nil {
		return false, err
	}
	found, err := s.rdb.SIsMember(ctx, s.key, raw).Result()
	if err != nil {
		return false, fmt.Errorf("redisx: contains %s: %w", s.key, err)
	}
	return found, nil
}

// Members reads every member, in no particular order. Use it on sets you know
// are small.
func (s Set[T]) Members(ctx context.Context) ([]T, error) {
	raws, err := s.rdb.SMembers(ctx, s.key).Result()
	if err != nil {
		return nil, fmt.Errorf("redisx: members of %s: %w", s.key, err)
	}
	return decodeAll(s.codec, raws)
}

// Len returns the number of members.
func (s Set[T]) Len(ctx context.Context) (int, error) {
	count, err := s.rdb.SCard(ctx, s.key).Result()
	if err != nil {
		return 0, fmt.Errorf("redisx: len %s: %w", s.key, err)
	}
	return int(count), nil
}

// Clear removes the whole set.
func (s Set[T]) Clear(ctx context.Context) error {
	if err := s.rdb.Del(ctx, s.key).Err(); err != nil {
		return fmt.Errorf("redisx: clear %s: %w", s.key, err)
	}
	return nil
}
