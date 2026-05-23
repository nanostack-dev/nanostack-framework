package ct

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

type ContainerSpec struct {
	Name         string
	Image        string
	ExposedPorts []string
	Env          map[string]string
}

type HarnessConfig struct {
	Postgres      *ContainerSpec
	Redis         *ContainerSpec
	WireMock      *ContainerSpec
	Env           map[string]string
	HealthURL     string
	HealthTimeout time.Duration
	HealthPoll    time.Duration
}

// ApplyEnv applies test environment variables before app startup.
func ApplyEnv(values map[string]string) error {
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return nil
}

// MustFreePort allocates a free local TCP port for dynamic test servers.
func MustFreePort() int {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("allocate free port: %v", err))
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		panic(fmt.Sprintf("unexpected listener address type: %T", listener.Addr()))
	}
	return addr.Port
}

// WaitForHTTP polls url until it returns a 2xx response or the context expires.
func WaitForHTTP(ctx context.Context, url string, poll time.Duration) error {
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	client := &http.Client{Timeout: poll}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}
