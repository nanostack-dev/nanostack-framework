package redistest

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nanostack-dev/nanostack-framework/pkg/redisx"
)

// Server is a server of its own for one test, for tests that break it: restart
// it, pause it or cut its connections. It always runs in a container, with no
// persistence, so a restart loses every key and every loaded script, as after
// a failover to a replica that missed the last writes. It keeps its address
// across restarts. The container is removed when the test ends.
//
// StartServer needs Docker. When the container cannot start and
// REDIS_TEST_ADDR is set, the test is skipped; otherwise it fails.
type Server struct {
	container testcontainers.Container
	addr      string
}

// StartServer starts a Server for t.
func StartServer(t testing.TB) *Server {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	port, err := freePort()
	if err != nil {
		t.Fatalf("redistest: %v", err)
	}
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image(),
			ExposedPorts: []string{"6379/tcp"},
			Cmd:          []string{"--save", "", "--appendonly", "no"},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = network.PortMap{
					network.MustParsePort("6379/tcp"): {{HostPort: strconv.Itoa(port)}},
				}
			},
			WaitingFor: wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		if os.Getenv("REDIS_TEST_ADDR") != "" {
			t.Skipf("redistest: StartServer needs Docker: %v", err)
		}
		t.Fatalf("redistest: start %s (is Docker running?): %v", image(), err)
	}
	s := &Server{container: ctr, addr: net.JoinHostPort("localhost", strconv.Itoa(port))}
	s.waitReady(t)
	return s
}

// Addr returns the address of the server. It stays the same after Restart.
func (s *Server) Addr() string { return s.addr }

// Client returns a go-redis client to the server, closed when the test ends.
func (s *Server) Client(t testing.TB) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: s.addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// New returns a redisx.Client on the server.
func (s *Server) New(t testing.TB) *redisx.Client {
	t.Helper()
	return redisx.New(s.Client(t), redisx.WithPrefix("test"))
}

// Restart stops the server and starts it again on the same address. Every key
// and every loaded script is lost, and every client connection is closed.
func (s *Server) Restart(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	stopTimeout := time.Duration(0)
	if err := s.container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("redistest: stop the server: %v", err)
	}
	if err := s.container.Start(ctx); err != nil {
		t.Fatalf("redistest: start the server again: %v", err)
	}
	s.waitReady(t)
}

// Pause freezes the server process. Connections stay open but nothing
// answers, as when a network partition or a long stall hits the server. The
// server resumes when the test ends if you do not call Resume.
func (s *Server) Pause(t testing.TB) {
	t.Helper()
	s.docker(t, func(ctx context.Context, docker *testcontainers.DockerClient, id string) error {
		_, err := docker.ContainerPause(ctx, id, client.ContainerPauseOptions{})
		return err
	})
	t.Cleanup(func() { s.resume() })
}

// Resume undoes Pause. The server then answers the commands that waited.
func (s *Server) Resume(t testing.TB) {
	t.Helper()
	if err := s.resume(); err != nil {
		t.Fatalf("redistest: resume the server: %v", err)
	}
}

// CutConnections closes every client connection on the server, both command
// and pub/sub connections, as when the network drops them. The server and its
// keys stay.
func (s *Server) CutConnections(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin := redis.NewClient(&redis.Options{Addr: s.addr})
	defer admin.Close()
	for _, kind := range []string{"normal", "pubsub"} {
		if err := admin.Do(ctx, "CLIENT", "KILL", "TYPE", kind, "SKIPME", "yes").Err(); err != nil {
			t.Fatalf("redistest: cut %s connections: %v", kind, err)
		}
	}
}

func (s *Server) resume() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	docker, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		return err
	}
	defer docker.Close()
	state, err := s.container.State(ctx)
	if err != nil || !state.Paused {
		return err
	}
	_, err = docker.ContainerUnpause(ctx, s.container.GetContainerID(), client.ContainerUnpauseOptions{})
	return err
}

func (s *Server) docker(t testing.TB, call func(context.Context, *testcontainers.DockerClient, string) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	docker, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Fatalf("redistest: connect to Docker: %v", err)
	}
	defer docker.Close()
	if err := call(ctx, docker, s.container.GetContainerID()); err != nil {
		t.Fatalf("redistest: %v", err)
	}
}

func (s *Server) waitReady(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(startTimeout)
	for {
		err := ping(s.addr)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("redistest: server not ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func image() string {
	if image := os.Getenv("REDIS_TEST_IMAGE"); image != "" {
		return image
	}
	return DefaultImage
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
