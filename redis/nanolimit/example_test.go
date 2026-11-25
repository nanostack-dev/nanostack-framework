package nanolimit_test

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// A per-user throttle and per-user concurrency slots on one Redis client.
func Example() {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	limits := nanolimit.New(client, nanolimit.WithFailurePolicy(nanolimit.FailOpen))

	calls, err := limits.RateLimiter("api:calls", nanolimit.PerMinute(120))
	if err != nil {
		panic(err)
	}
	decision, err := calls.Allow(ctx, "user-1")
	if err != nil {
		fmt.Println("limit unchecked:", err) // decision follows the policy
	}
	if !decision.Allowed {
		fmt.Println("429, Retry-After:", decision.RetryAfter.Round(time.Second))
	}

	runs, err := limits.Semaphore("api:runs", 2)
	if err != nil {
		panic(err)
	}
	permit, err := runs.Acquire(ctx, "user-1", 5*time.Minute)
	if err == nil && permit.Acquired {
		defer func() { _ = runs.Release(context.WithoutCancel(ctx), "user-1", permit.ID) }()
	}
}
