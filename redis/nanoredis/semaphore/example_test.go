package semaphore_test

import (
	"context"
	"log/slog"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/semaphore"
)

// At most two concurrent runs per user. A run that crashes frees its permit
// when its five-minute lease ends.
func Example() {
	ctx := context.Background()
	store := cache.NewStore(ctx, cache.Config{Address: "localhost:6379"}, slog.Default())

	runs, err := semaphore.New(store, "api:runs", 2)
	if err != nil {
		panic(err)
	}
	permit, err := runs.Acquire(ctx, "user-1", 5*time.Minute)
	if err != nil || !permit.Acquired {
		return // busy, or Redis did not answer (FailOpen grants without an ID)
	}
	defer func() { _ = runs.Release(context.WithoutCancel(ctx), "user-1", permit.ID) }()
}
