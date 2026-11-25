package throttle_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/throttle"
)

// A per-user throttle over the cache's Redis that admits when Redis is down.
func Example() {
	ctx := context.Background()
	logger := slog.Default()
	store := cache.NewStore(ctx, cache.Config{Address: "localhost:6379"}, logger)

	calls, err := throttle.New(store, "api:calls", throttle.PerMinute(120),
		nanoredis.WithFailurePolicy(nanoredis.FailOpen), nanoredis.WithLogger(logger))
	if err != nil {
		panic(err)
	}
	decision, err := calls.Allow(ctx, "user-1")
	if err != nil {
		fmt.Println("limit unchecked:", err) // the decision follows the policy
	}
	if !decision.Allowed {
		fmt.Println("429, Retry-After:", decision.RetryAfter.Round(time.Second))
	}
}
