//go:build integration

package nanocache_test

import (
	"errors"
	"strconv"
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

	products := mustNew[string](store, namespace+":product", time.Minute)
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

// EvictPrefix must remove exactly its own scope, whatever the parts hold.
func TestEvictPrefixRemovesExactlyItsScope(t *testing.T) {
	namespace := redistest.Namespace(t)
	store := nanocache.NewStore(t.Context(), nanocache.Config{Address: redistest.Addr(t)}, discard())
	t.Cleanup(func() { _ = store.Close() })
	perms := mustNew[string](store, namespace+":perms", time.Minute)
	scopes := []string{"team[ops]", "teamo", "*", "b", `a\b`, "a:b", "a"}
	for _, scope := range scopes {
		value := "v"
		if err := perms.Key(scope, "role").Set(t.Context(), &value); err != nil {
			t.Fatalf("Set %q: %v", scope, err)
		}
	}
	present := func(scope string) bool {
		found, err := perms.Key(scope, "role").Exists(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	for _, evict := range []string{"team[ops]", "*", `a\b`, "a:b"} {
		if err := perms.EvictPrefix(t.Context(), evict); err != nil {
			t.Fatalf("EvictPrefix %q: %v", evict, err)
		}
		if present(evict) {
			t.Errorf("EvictPrefix(%q) left its own entry", evict)
		}
	}
	for _, kept := range []string{"teamo", "b", "a"} {
		if !present(kept) {
			t.Errorf("entry of scope %q was evicted by another scope's EvictPrefix", kept)
		}
	}
}

// EvictPattern unlinks page by page: more keys than one SCAN page are all
// removed.
func TestEvictPrefixRemovesMoreThanOnePage(t *testing.T) {
	namespace := redistest.Namespace(t)
	store := nanocache.NewStore(t.Context(), nanocache.Config{Address: redistest.Addr(t)}, discard())
	t.Cleanup(func() { _ = store.Close() })
	items := mustNew[string](store, namespace+":items", time.Minute)
	const n = 1200
	pipe := store.RedisClient().Pipeline()
	for i := range n {
		pipe.Set(t.Context(), items.Key("tenant", strconv.Itoa(i)).String(), `"v"`, time.Minute)
	}
	if _, err := pipe.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := items.EvictPrefix(t.Context(), "tenant"); err != nil {
		t.Fatal(err)
	}
	left := 0
	iter := store.RedisClient().Scan(t.Context(), 0, namespace+":items:*", 1000).Iterator()
	for iter.Next(t.Context()) {
		left++
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d keys left after EvictPrefix, want 0", left)
	}
}
