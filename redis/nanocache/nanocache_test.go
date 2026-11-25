package nanocache_test

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
)

// The forwarder must hand out the very values of its new home, so the two
// import paths mix during the move.
func TestForwarderSharesTheValuesOfCache(t *testing.T) {
	if !errors.Is(nanocache.ErrCacheKeyNotFound, cache.ErrCacheKeyNotFound) ||
		!errors.Is(nanocache.ErrInvalidPrefix, cache.ErrInvalidPrefix) {
		t.Error("the forwarded errors are not the errors of cache")
	}
	store := nanocache.NoOp()
	if _, err := nanocache.New[string](store, "", time.Minute, slog.New(slog.DiscardHandler)); !errors.Is(
		err, cache.ErrInvalidPrefix) {
		t.Errorf("New with an empty prefix = %v, want cache.ErrInvalidPrefix", err)
	}
}
