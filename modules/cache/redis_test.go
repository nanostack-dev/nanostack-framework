package cache_test

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/nanostack-dev/nanostack-framework/modules/cache"
)

// TestNewRedisCacheUnreachableReturnsError verifies an unreachable Redis yields
// an error (not a panic), so the module can fall back to a no-op cache.
func TestNewRedisCacheUnreachableReturnsError(t *testing.T) {
	// Port 1 is reserved/unused → immediate connection refused, well under the
	// ping timeout.
	c, err := cache.NewRedisCache(cache.Config{Address: "127.0.0.1:1"}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error connecting to unreachable redis, got nil")
	}
	if c != nil {
		t.Fatalf("expected nil cache on connect failure, got %v", c)
	}
}

// TestNoOpCacheGetOrElseRunsFallback verifies the no-op cache never stores and
// always evaluates the fallback — i.e. callers always see fresh data.
func TestNoOpCacheGetOrElseRunsFallback(t *testing.T) {
	noop := cache.NewNoOpCache()
	calls := 0
	loader := func() (string, error) {
		calls++
		return "fresh", nil
	}
	for range 2 {
		got, err := noop.GetOrElse(context.Background(), "k", loader, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "fresh" {
			t.Fatalf("got %q, want fresh", got)
		}
	}
	if calls != 2 {
		t.Fatalf("expected fallback run on every call (no caching), got %d calls", calls)
	}
}
