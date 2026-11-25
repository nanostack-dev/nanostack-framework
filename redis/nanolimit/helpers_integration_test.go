//go:build integration

package nanolimit_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// The tests run against the shared server of redis/internal/redistest:
//
//	go test -tags=integration ./redis/nanolimit/
//
// Each test owns a key namespace, so the tests run in parallel; the names
// of Redisson's tests are kept inside it.

type env struct {
	t      *testing.T
	ns     string
	rdb    *redis.Client
	limits *nanolimit.Limits
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ns := redistest.Namespace(t)
	rdb := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = rdb.Close() })
	return &env{t: t, ns: ns, rdb: rdb, limits: nanolimit.New(rdb, discard())}
}

// client is another process: its own client and its own Limits.
func (e *env) client(opts ...nanolimit.Option) *nanolimit.Limits {
	e.t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: redistest.Addr(e.t)})
	e.t.Cleanup(func() { _ = rdb.Close() })
	return nanolimit.New(rdb, discard(), opts...)
}

func (e *env) name(name string) string { return e.ns + ":" + name }

func (e *env) rate(name string) nanolimit.RateLimiter {
	return e.limits.RateLimiter(e.name(name))
}

func (e *env) sem(name string) nanolimit.Semaphore { return e.limits.Semaphore(e.name(name)) }

// keyCount is Redisson's getKeys().count() for this test's namespace.
func (e *env) keyCount() int {
	e.t.Helper()
	keys, err := e.rdb.Keys(e.t.Context(), "*"+e.ns+"*").Result()
	if err != nil {
		e.t.Fatalf("KEYS: %v", err)
	}
	return len(keys)
}

func ctx(t *testing.T) context.Context { return t.Context() }

// result carries the two values of a call into must: Go spreads a call's
// results only when it is the sole argument.
type result[T any] struct {
	v   T
	err error
}

func of[T any](v T, err error) result[T] { return result[T]{v, err} }

func must[T any](t *testing.T, r result[T]) T {
	t.Helper()
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	return r.v
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func eq[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func between(t *testing.T, got, lo, hi time.Duration, what string) {
	t.Helper()
	if got < lo || got > hi {
		t.Fatalf("%s = %v, want between %v and %v", what, got, lo, hi)
	}
}

// eventually polls cond every 10ms until it holds or within passes
// (Awaitility).
func eventually(t *testing.T, within time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not hold within %v", what, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
