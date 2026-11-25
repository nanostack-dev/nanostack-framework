package cache_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
)

type plan struct {
	Name string `json:"name"`
}

// Without an address, NewStore returns the no-op store: every read calls the
// loader. With Redis, the second read is a hit.
func ExampleNew() {
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)
	store := cache.NewStore(ctx, cache.Config{}, logger)
	plans, err := cache.New[plan](store, "plan", 10*time.Minute, logger)
	if err != nil {
		panic(err)
	}

	got, err := plans.Key("acme").GetOrElse(ctx, func() (*plan, error) { return &plan{Name: "pro"}, nil })
	fmt.Println(got.Name, err)
	// Output: pro <nil>
}
