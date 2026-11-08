package nanocache_test

import (
	"log/slog"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestNewStoreWithoutAnAddressIsTheNoOpStore(t *testing.T) {
	store := nanocache.NewStore(t.Context(), nanocache.Config{}, discard())
	if store.RedisClient() != nil {
		t.Fatal("NewStore without an address returned a Redis store")
	}
	if _, ok := store.(nano.HealthChecker); ok {
		t.Fatal("the no-op store must not be a readiness probe")
	}
}

func TestNewStoreFallsBackWhenRedisIsUnreachable(t *testing.T) {
	// Port 1 refuses the connection at once.
	store := nanocache.NewStore(t.Context(), nanocache.Config{Address: "127.0.0.1:1"}, discard())
	if store.RedisClient() != nil {
		t.Fatal("NewStore returned a Redis store for an unreachable server")
	}
}

func TestNoOpStoreRunsTheFallbackEveryTime(t *testing.T) {
	store := nanocache.NoOp()
	calls := 0
	for range 2 {
		got, err := store.GetOrElse(t.Context(), "k", func() (string, error) { calls++; return "fresh", nil }, 0)
		if err != nil || got != "fresh" {
			t.Fatalf("GetOrElse = (%q, %v), want (fresh, nil)", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("fallback ran %d times, want 2: the no-op store caches nothing", calls)
	}
}
