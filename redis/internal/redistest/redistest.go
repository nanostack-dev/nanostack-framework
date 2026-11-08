// Package redistest gives the integration tests of the redis modules a real
// Redis or Valkey server.
//
// The server starts on first use and is shared by every test of the test
// binary. Each test gets its own key namespace, emptied when the test ends,
// so tests can run in parallel against the one server.
//
// Set REDIS_TEST_ADDR to use a server you run yourself. Otherwise the package
// starts a container (REDIS_TEST_IMAGE, default DefaultImage) through
// testcontainers, which removes it when the test binary exits.
//
// It is the layered copy of the two functions of pkg/testkit/redistest that
// the redis modules use: that package depends on pkg/redisx, which carries
// zerolog, so a layered package may not import it (internal/layers).
package redistest

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// DefaultImage is the server image started when REDIS_TEST_IMAGE is empty.
	DefaultImage = "valkey/valkey:8.1-alpine"

	startTimeout = 2 * time.Minute
)

//nolint:gochecknoglobals // One server per test binary is the point of the package.
var server = sync.OnceValues(start)

// Addr returns the address of the shared server, and starts it on first use.
// The test fails when the server cannot start.
func Addr(t testing.TB) string {
	t.Helper()
	addr, err := server()
	if err != nil {
		t.Fatalf("redistest: %v", err)
	}
	return addr
}

// Namespace returns a key prefix unique to the test. When the test ends, every
// key that contains it is deleted.
func Namespace(t testing.TB) string {
	t.Helper()
	namespace := "test-" + strings.ToLower(rand.Text()[:12])
	rdb := redis.NewClient(&redis.Options{Addr: Addr(t)})
	t.Cleanup(func() {
		defer func() { _ = rdb.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		iter := rdb.Scan(ctx, 0, "*"+namespace+"*", 500).Iterator()
		for iter.Next(ctx) {
			if err := rdb.Del(ctx, iter.Val()).Err(); err != nil {
				t.Errorf("redistest: delete %s: %v", iter.Val(), err)
			}
		}
		if err := iter.Err(); err != nil {
			t.Errorf("redistest: scan %s: %v", namespace, err)
		}
	})
	return namespace
}

func start() (string, error) {
	if addr := os.Getenv("REDIS_TEST_ADDR"); addr != "" {
		return addr, ping(addr)
	}
	image := os.Getenv("REDIS_TEST_IMAGE")
	if image == "" {
		image = DefaultImage
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Image:        image,
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
		Started:      true,
	})
	if err != nil {
		return "", fmt.Errorf(
			"start %s (is Docker running? set REDIS_TEST_ADDR to use your own server): %w", image, err,
		)
	}
	endpoint, err := container.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		return "", fmt.Errorf("read the address of %s: %w", image, err)
	}
	return endpoint, ping(endpoint)
}

func ping(addr string) error {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping %s: %w", addr, err)
	}
	return nil
}
