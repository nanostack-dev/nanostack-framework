package throttle_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/internal/redistest"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/cache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanoredis/throttle"
)

// These tests need no Redis: they cover the configuration, the answers of
// each FailurePolicy, the deadline and the warning.

func TestInvalidConfigurationIsRefused(t *testing.T) {
	for name, rate := range map[string]throttle.Rate{
		"zero limit":     {Limit: 0, Period: time.Second},
		"zero period":    {Limit: 1},
		"negative burst": throttle.PerSecond(1).WithBurst(-1),
		"sub-µs spacing": {Limit: 2_000_000, Period: time.Second},
	} {
		if _, err := throttle.NewWithClient(nil, "calls", rate); !errors.Is(err, nanoredis.ErrInvalidArgument) {
			t.Errorf("%s: New error = %v, want ErrInvalidArgument", name, err)
		}
	}
	for _, name := range []string{"", "calls:", "calls*", "a[b]"} {
		if _, err := throttle.NewWithClient(nil, name, throttle.PerSecond(1)); !errors.Is(
			err, nanoredis.ErrInvalidArgument) {
			t.Errorf("New(%q) error = %v, want ErrInvalidArgument", name, err)
		}
	}
}

func TestInvalidCallIsRefusedWithoutPolicy(t *testing.T) {
	limiter := mustNew(t, nil, throttle.PerSecond(5).WithBurst(2)) // FailOpen: a bad call is still refused.
	for name, n := range map[string]int{"zero": 0, "above burst": 3} {
		decision, err := limiter.AllowN(t.Context(), "k", n)
		if !errors.Is(err, nanoredis.ErrInvalidArgument) || decision.Allowed {
			t.Errorf("%s: AllowN = %+v, %v; want a refusal and ErrInvalidArgument", name, decision, err)
		}
	}
	decision, err := limiter.Allow(t.Context(), "")
	if !errors.Is(err, nanoredis.ErrInvalidArgument) || decision.Allowed {
		t.Errorf("Allow of an empty key = %+v, %v; want a refusal and ErrInvalidArgument", decision, err)
	}
}

func TestWithoutRedisThePolicyAnswers(t *testing.T) {
	for _, tt := range []struct {
		name   string
		client func(t *testing.T) *redis.Client
		opts   []nanoredis.Option
		want   bool
	}{
		{"no client, default policy", noClient, nil, true},
		{"no client, fail-closed", noClient, []nanoredis.Option{nanoredis.WithFailurePolicy(nanoredis.FailClosed)}, false},
		{"redis down, fail-open", downClient, []nanoredis.Option{nanoredis.WithFailurePolicy(nanoredis.FailOpen)}, true},
		{"redis down, fail-closed", downClient, []nanoredis.Option{nanoredis.WithFailurePolicy(nanoredis.FailClosed)}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			limiter := mustNew(t, tt.client(t), throttle.PerSecond(1), tt.opts...)
			decision, err := limiter.Allow(t.Context(), "k")
			if !errors.Is(err, nanoredis.ErrUnavailable) || decision.Allowed != tt.want {
				t.Errorf("Allow = %+v, %v; want Allowed %v and ErrUnavailable", decision, err, tt.want)
			}
		})
	}
}

func TestNoOpStoreHasNoRedis(t *testing.T) {
	limiter, err := throttle.New(cache.NoOp(), "calls", throttle.PerSecond(1))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := limiter.Allow(t.Context(), "k")
	if !errors.Is(err, nanoredis.ErrUnavailable) || !decision.Allowed {
		t.Errorf("Allow over the no-op store = %+v, %v; want admitted and ErrUnavailable", decision, err)
	}
}

// A server that accepts connections and never answers holds a client with
// default options for its 3-second read timeout. The call must answer by the
// policy at its own deadline instead, whether or not the client applies
// context deadlines itself.
func TestDeadlineBoundsASilentRedis(t *testing.T) {
	addr := redistest.SilentServer(t)
	for name, contextTimeouts := range map[string]bool{
		"client ignores deadlines": false, "client applies deadlines": true,
	} {
		t.Run(name, func(t *testing.T) {
			client := redis.NewClient(
				&redis.Options{Addr: addr, MaxRetries: -1, ContextTimeoutEnabled: contextTimeouts},
			)
			t.Cleanup(func() { _ = client.Close() })
			limiter := mustNew(t, client, throttle.PerSecond(1), nanoredis.WithTimeout(50*time.Millisecond))
			start := time.Now()
			decision, err := limiter.Allow(t.Context(), "k")
			elapsed := time.Since(start)
			if !errors.Is(err, nanoredis.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Allow error = %v, want ErrUnavailable wrapping context.DeadlineExceeded", err)
			}
			if !decision.Allowed {
				t.Error("FailOpen refused the event of a silent Redis")
			}
			if elapsed > time.Second {
				t.Errorf("Allow took %s against a silent Redis, want about its 50ms deadline", elapsed)
			}
		})
	}
}

func TestFailureWarningIsThrottled(t *testing.T) {
	var out syncBuffer
	limiter := mustNew(t, nil, throttle.PerSecond(1), nanoredis.WithLogger(slog.New(slog.NewTextHandler(&out, nil))))
	for range 10 {
		_, _ = limiter.Allow(t.Context(), "k")
	}
	if got := strings.Count(out.String(), "could not reach redis"); got != 1 {
		t.Errorf("logged %d warnings for 10 failures in a minute, want 1:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "policy=fail-open") || !strings.Contains(out.String(), "limit=calls") {
		t.Errorf("the warning does not name the limit and its policy:\n%s", out.String())
	}
}

func mustNew(t *testing.T, client *redis.Client, rate throttle.Rate, opts ...nanoredis.Option) throttle.Limiter {
	t.Helper()
	limiter, err := throttle.NewWithClient(client, "calls", rate, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func noClient(*testing.T) *redis.Client { return nil }

func downClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redistest.ClosedAddr(t), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
