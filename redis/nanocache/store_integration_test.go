//go:build integration

package nanocache_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

// Only a real server shows that NewStore connects, that EvictPattern finds
// keys with SCAN and that Stop closes the client. The server is the shared
// one of redis/internal/redistest:
//
//	go test -tags=integration ./redis/nanocache/

func TestNewStoreUsesRedisAndStops(t *testing.T) {
	namespace := redistest.Namespace(t)
	store := nanocache.NewStore(t.Context(), nanocache.Config{Address: redistest.Addr(t)}, discard())
	if store.RedisClient() == nil {
		t.Fatal("NewStore returned the no-op store for a reachable server")
	}
	checker, ok := store.(nano.HealthChecker)
	if !ok {
		t.Fatal("the Redis store must be a readiness probe")
	}
	if err := checker.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}

	products := nanocache.New[string](store, namespace+":product", time.Minute, discard())
	for _, id := range []string{"a", "b"} {
		value := "v-" + id
		if err := products.Key("tenant", id).Set(t.Context(), &value); err != nil {
			t.Fatalf("Set %s: %v", id, err)
		}
	}
	if err := products.EvictPrefix(t.Context(), "tenant"); err != nil {
		t.Fatalf("EvictPrefix: %v", err)
	}
	if _, err := products.Key("tenant", "a").Get(t.Context()); !errors.Is(err, nanocache.ErrCacheKeyNotFound) {
		t.Fatalf("Get after EvictPrefix = %v, want ErrCacheKeyNotFound", err)
	}

	stopper, ok := store.(nano.Stopper)
	if !ok {
		t.Fatal("the Redis store must be a Stopper")
	}
	if err := stopper.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := checker.CheckHealth(t.Context()); err == nil {
		t.Fatal("CheckHealth succeeded after Stop closed the client")
	}
}
