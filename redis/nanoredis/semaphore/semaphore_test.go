package semaphore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/semaphore"
)

// These tests need no Redis: they cover the configuration and the answers
// of each FailurePolicy. The deadline and the warning are shared code,
// tested in package throttle.

func TestInvalidConfigurationIsRefused(t *testing.T) {
	if _, err := semaphore.NewWithClient(nil, "slots", 0); !errors.Is(err, nanoredis.ErrInvalidArgument) {
		t.Errorf("New with 0 permits error = %v, want ErrInvalidArgument", err)
	}
	for _, name := range []string{"", "slots:", "slots*"} {
		if _, err := semaphore.NewWithClient(nil, name, 1); !errors.Is(err, nanoredis.ErrInvalidArgument) {
			t.Errorf("New(%q) error = %v, want ErrInvalidArgument", name, err)
		}
	}
}

func TestInvalidCallIsRefusedWithoutPolicy(t *testing.T) {
	slots := mustNew(t, nil, 1)
	permit, err := slots.Acquire(t.Context(), "k", 0)
	if !errors.Is(err, nanoredis.ErrInvalidArgument) || permit.Acquired {
		t.Errorf("Acquire with no lease = %+v, %v; want a refusal and ErrInvalidArgument", permit, err)
	}
	if permit, err = slots.Acquire(t.Context(), "", time.Second); !errors.Is(
		err, nanoredis.ErrInvalidArgument) || permit.Acquired {
		t.Errorf("Acquire of an empty key = %+v, %v; want a refusal and ErrInvalidArgument", permit, err)
	}
	if err = slots.Extend(t.Context(), "k", "id", 0); !errors.Is(err, nanoredis.ErrInvalidArgument) {
		t.Errorf("Extend with no lease error = %v, want ErrInvalidArgument", err)
	}
}

func TestWithoutRedisThePolicyAnswers(t *testing.T) {
	down := redis.NewClient(&redis.Options{Addr: redistest.ClosedAddr(t), MaxRetries: -1})
	t.Cleanup(func() { _ = down.Close() })
	for _, tt := range []struct {
		name   string
		client *redis.Client
		policy nanoredis.FailurePolicy
		want   bool
	}{
		{"no client, fail-open", nil, nanoredis.FailOpen, true},
		{"no client, fail-closed", nil, nanoredis.FailClosed, false},
		{"redis down, fail-open", down, nanoredis.FailOpen, true},
		{"redis down, fail-closed", down, nanoredis.FailClosed, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			slots := mustNew(t, tt.client, 1, nanoredis.WithFailurePolicy(tt.policy))
			ctx := t.Context()
			permit, err := slots.Acquire(ctx, "k", time.Second)
			if !errors.Is(err, nanoredis.ErrUnavailable) || permit.Acquired != tt.want || permit.ID != "" {
				t.Errorf("Acquire = %+v, %v; want Acquired %v, no ID and ErrUnavailable", permit, err, tt.want)
			}
			if err = slots.Release(ctx, "k", permit.ID); err != nil {
				t.Errorf("Release of a permit without ID = %v, want nil", err)
			}
			if err = slots.Release(ctx, "k", "some-id"); !errors.Is(err, nanoredis.ErrUnavailable) {
				t.Errorf("Release = %v, want ErrUnavailable", err)
			}
			if err = slots.Extend(ctx, "k", "some-id", time.Second); !errors.Is(err, nanoredis.ErrUnavailable) {
				t.Errorf("Extend = %v, want ErrUnavailable", err)
			}
			if n, availableErr := slots.Available(ctx, "k"); !errors.Is(
				availableErr, nanoredis.ErrUnavailable) || n != 0 {
				t.Errorf("Available = %d, %v; want 0 and ErrUnavailable", n, availableErr)
			}
		})
	}
}

func TestNoOpStoreHasNoRedis(t *testing.T) {
	slots, err := semaphore.New(cache.NoOp(), "slots", 1)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := slots.Acquire(t.Context(), "k", time.Second)
	if !errors.Is(err, nanoredis.ErrUnavailable) || !permit.Acquired {
		t.Errorf("Acquire over the no-op store = %+v, %v; want granted and ErrUnavailable", permit, err)
	}
}

func TestCallerContextBoundsTheCall(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: redistest.SilentServer(t), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	slots := mustNew(t, client, 1, nanoredis.WithTimeout(0), nanoredis.WithFailurePolicy(nanoredis.FailClosed))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	permit, err := slots.Acquire(ctx, "k", time.Second)
	if !errors.Is(err, context.DeadlineExceeded) || permit.Acquired {
		t.Fatalf("Acquire = %+v, %v; want a refusal wrapping context.DeadlineExceeded", permit, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Acquire took %s, want about the caller's 50ms deadline", elapsed)
	}
}

func mustNew(t *testing.T, client *redis.Client, permits int, opts ...nanoredis.Option) semaphore.Pool {
	t.Helper()
	slots, err := semaphore.NewWithClient(client, "slots", permits, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return slots
}
