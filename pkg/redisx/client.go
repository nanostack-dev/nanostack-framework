// Package redisx gives typed shared state on Redis or Valkey: values, maps,
// sets, queues, counters, a mutex with fencing tokens, and pub/sub topics.
// See README.md for the guarantees of each.
package redisx

import (
	"errors"
	"strings"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrLocked is returned by TryLock when another holder has the mutex.
	ErrLocked = errors.New("redisx: mutex is held by another holder")
	// ErrLeaseLost is returned when a lease expired or was taken over before
	// its holder refreshed or released it.
	ErrLeaseLost = errors.New("redisx: lease lost")
	// ErrConflict is returned by Value.Update when concurrent writers kept
	// changing the value for every attempt.
	ErrConflict = errors.New("redisx: value changed concurrently on every attempt")
)

// Client is the entry point to the shared data structures, mutexes and topics
// of one Redis or Valkey deployment. It is cheap to copy and safe for
// concurrent use.
type Client struct {
	rdb    redis.UniversalClient
	prefix string
}

// Option configures a Client.
type Option func(*Client)

// WithPrefix puts every key and channel of the Client under prefix, so that
// several applications or tests can share one server.
func WithPrefix(prefix string) Option {
	return func(c *Client) { c.prefix = strings.TrimSuffix(prefix, ":") }
}

// New wraps a go-redis client. A *redis.Client, *redis.ClusterClient or
// *redis.Ring all work. The Client does not own rdb: close it yourself.
func New(rdb redis.UniversalClient, opts ...Option) *Client {
	c := &Client{rdb: rdb}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Redis returns the underlying go-redis client for commands this package does
// not cover.
func (c *Client) Redis() redis.UniversalClient { return c.rdb }

// Key returns the full key name for name, prefix included.
func (c *Client) Key(name string) string {
	if c.prefix == "" {
		return name
	}
	return c.prefix + ":" + name
}
