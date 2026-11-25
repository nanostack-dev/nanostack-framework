package nanolimit_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
	"github.com/nanostack-dev/nanostack-framework/redis/nanolimit"
)

// These tests need no Redis: they cover the configuration, the answers of
// each FailurePolicy, the deadline and the warning.

func TestInvalidConfigurationIsRefused(t *testing.T) {
	limits := nanolimit.New(nil)
	for name, rate := range map[string]nanolimit.Rate{
		"zero limit":     {Limit: 0, Period: time.Second},
		"zero period":    {Limit: 1},
		"negative burst": nanolimit.PerSecond(1).WithBurst(-1),
		"sub-µs spacing": {Limit: 2_000_000, Period: time.Second},
	} {
		if _, err := limits.RateLimiter("calls", rate); !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Errorf("%s: RateLimiter error = %v, want ErrInvalidArgument", name, err)
		}
	}
	for _, name := range []string{"", "calls:", "calls*", "a[b]"} {
		if _, err := limits.RateLimiter(name, nanolimit.PerSecond(1)); !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Errorf("RateLimiter(%q) error = %v, want ErrInvalidArgument", name, err)
		}
		if _, err := limits.Semaphore(name, 1); !errors.Is(err, nanolimit.ErrInvalidArgument) {
			t.Errorf("Semaphore(%q) error = %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := limits.Semaphore("slots", 0); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Errorf("Semaphore with 0 permits error = %v, want ErrInvalidArgument", err)
	}
}

func TestInvalidCallIsRefusedWithoutPolicy(t *testing.T) {
	limits := nanolimit.New(nil) // FailOpen: a bad call must still not be admitted.
	limiter := mustRate(t, limits, "calls", nanolimit.PerSecond(5).WithBurst(2))
	for name, n := range map[string]int{"zero": 0, "above burst": 3} {
		decision, err := limiter.AllowN(t.Context(), "k", n)
		if !errors.Is(err, nanolimit.ErrInvalidArgument) || decision.Allowed {
			t.Errorf("%s: AllowN = %+v, %v; want a refusal and ErrInvalidArgument", name, decision, err)
		}
	}
	empty, err := limiter.Allow(t.Context(), "")
	if !errors.Is(err, nanolimit.ErrInvalidArgument) || empty.Allowed {
		t.Errorf("Allow of an empty key = %+v, %v; want a refusal and ErrInvalidArgument", empty, err)
	}
	slots := mustSemaphore(t, limits, "slots", 1)
	permit, err := slots.Acquire(t.Context(), "k", 0)
	if !errors.Is(err, nanolimit.ErrInvalidArgument) || permit.Acquired {
		t.Errorf("Acquire with no lease = %+v, %v; want a refusal and ErrInvalidArgument", permit, err)
	}
	if err = slots.Extend(t.Context(), "k", "id", 0); !errors.Is(err, nanolimit.ErrInvalidArgument) {
		t.Errorf("Extend with no lease error = %v, want ErrInvalidArgument", err)
	}
}

func TestNoRedisAppliesThePolicy(t *testing.T) {
	for _, tt := range []struct {
		policy nanolimit.FailurePolicy
		want   bool
	}{{nanolimit.FailOpen, true}, {nanolimit.FailClosed, false}} {
		t.Run(tt.policy.String(), func(t *testing.T) {
			limits := nanolimit.FromStore(nanocache.NoOp(), nanolimit.WithFailurePolicy(tt.policy))
			assertPolicy(t, limits, tt.want)
		})
	}
}

func TestDefaultPolicyIsFailOpen(t *testing.T) {
	assertPolicy(t, nanolimit.New(nil), true)
}

func TestRedisDownAppliesThePolicy(t *testing.T) {
	addr := closedAddr(t)
	for _, tt := range []struct {
		policy nanolimit.FailurePolicy
		want   bool
	}{{nanolimit.FailOpen, true}, {nanolimit.FailClosed, false}} {
		t.Run(tt.policy.String(), func(t *testing.T) {
			client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
			t.Cleanup(func() { _ = client.Close() })
			assertPolicy(t, nanolimit.New(client, nanolimit.WithFailurePolicy(tt.policy)), tt.want)
		})
	}
}

// A server that accepts connections and never answers holds a client with
// default options for its 3-second read timeout. The call must answer by the
// policy at its own deadline instead, whether or not the client applies
// context deadlines itself.
func TestDeadlineBoundsASilentRedis(t *testing.T) {
	addr := silentServer(t)
	for name, contextTimeouts := range map[string]bool{"client ignores deadlines": false, "client applies deadlines": true} {
		t.Run(name, func(t *testing.T) {
			client := redis.NewClient(
				&redis.Options{Addr: addr, MaxRetries: -1, ContextTimeoutEnabled: contextTimeouts},
			)
			t.Cleanup(func() { _ = client.Close() })
			limits := nanolimit.New(client, nanolimit.WithTimeout(50*time.Millisecond))
			limiter := mustRate(t, limits, "calls", nanolimit.PerSecond(1))
			start := time.Now()
			decision, err := limiter.Allow(t.Context(), "k")
			elapsed := time.Since(start)
			if !errors.Is(err, nanolimit.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
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

func TestCallerContextAlsoBoundsTheCall(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: silentServer(t), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	limits := nanolimit.New(client, nanolimit.WithTimeout(0), nanolimit.WithFailurePolicy(nanolimit.FailClosed))
	slots := mustSemaphore(t, limits, "slots", 1)
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

func TestFailureWarningIsThrottled(t *testing.T) {
	var out syncBuffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	limits := nanolimit.New(nil, nanolimit.WithLogger(logger))
	limiter := mustRate(t, limits, "calls", nanolimit.PerSecond(1))
	slots := mustSemaphore(t, limits, "slots", 1)
	for range 5 {
		_, _ = limiter.Allow(t.Context(), "k")
		_, _ = slots.Acquire(t.Context(), "k", time.Second)
	}
	if got := strings.Count(out.String(), "could not reach redis"); got != 1 {
		t.Errorf("logged %d warnings for 10 failures in a minute, want 1:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "policy=fail-open") {
		t.Errorf("the warning does not name the policy:\n%s", out.String())
	}
}

func TestNewLimitsWarnsWithoutRedis(t *testing.T) {
	var out syncBuffer
	limits := nanolimit.NewLimits(t.Context(), nanocache.NoOp(), slog.New(slog.NewTextHandler(&out, nil)))
	if limits == nil || !strings.Contains(out.String(), "no redis for shared limits") {
		t.Errorf("NewLimits over the no-op store logged %q, want the no-redis warning", out.String())
	}
}

// assertPolicy checks the answers of every call of a Limits whose Redis does
// not answer.
func assertPolicy(t *testing.T, limits *nanolimit.Limits, allowed bool) {
	t.Helper()
	ctx := t.Context()
	decision, err := mustRate(t, limits, "calls", nanolimit.PerSecond(1)).Allow(ctx, "k")
	if !errors.Is(err, nanolimit.ErrUnavailable) || decision.Allowed != allowed {
		t.Errorf("Allow = %+v, %v; want Allowed %v and ErrUnavailable", decision, err, allowed)
	}
	slots := mustSemaphore(t, limits, "slots", 1)
	permit, err := slots.Acquire(ctx, "k", time.Second)
	if !errors.Is(err, nanolimit.ErrUnavailable) || permit.Acquired != allowed || permit.ID != "" {
		t.Errorf("Acquire = %+v, %v; want Acquired %v, no ID and ErrUnavailable", permit, err, allowed)
	}
	if err = slots.Release(ctx, "k", permit.ID); err != nil {
		t.Errorf("Release of a permit without ID = %v, want nil", err)
	}
	if err = slots.Release(ctx, "k", "some-id"); !errors.Is(err, nanolimit.ErrUnavailable) {
		t.Errorf("Release = %v, want ErrUnavailable", err)
	}
	if err = slots.Extend(ctx, "k", "some-id", time.Second); !errors.Is(err, nanolimit.ErrUnavailable) {
		t.Errorf("Extend = %v, want ErrUnavailable", err)
	}
	if n, availableErr := slots.Available(ctx, "k"); !errors.Is(availableErr, nanolimit.ErrUnavailable) || n != 0 {
		t.Errorf("Available = %d, %v; want 0 and ErrUnavailable", n, availableErr)
	}
}

func mustRate(t *testing.T, limits *nanolimit.Limits, name string, rate nanolimit.Rate) nanolimit.RateLimiter {
	t.Helper()
	limiter, err := limits.RateLimiter(name, rate)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func mustSemaphore(t *testing.T, limits *nanolimit.Limits, name string, permits int) nanolimit.Semaphore {
	t.Helper()
	semaphore, err := limits.Semaphore(name, permits)
	if err != nil {
		t.Fatal(err)
	}
	return semaphore
}

// closedAddr returns an address where nothing listens.
func closedAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

// silentServer accepts connections, reads them and never writes.
func silentServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})
	wg.Go(func() {
		var conns []net.Conn
		defer func() {
			for _, conn := range conns {
				_ = conn.Close()
			}
		}()
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			conns = append(conns, conn)
		}
	})
	return listener.Addr().String()
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
