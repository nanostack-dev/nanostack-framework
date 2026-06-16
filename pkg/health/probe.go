package health

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	defaultProbePort    = "8080"
	defaultProbeTimeout = 3 * time.Second
)

// LocalHealthURL returns the URL of the local server's health endpoint, built
// from SERVER_PORT (the same env the HTTP server binds; default 8080) and
// DefaultPath. It targets the loopback interface so the probe stays inside the
// container.
func LocalHealthURL() string {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = defaultProbePort
	}
	return fmt.Sprintf("http://127.0.0.1:%s%s", port, DefaultPath)
}

// Probe performs a single HTTP GET against the local health endpoint and returns
// an error if the server is unreachable or reports a non-2xx status.
//
// It is the client counterpart to the server-side Handler: a container can run
// `<binary> -healthcheck` -> Probe so the runtime / Swarm detects a hung process
// and restarts it. This matters most for distroless images, which ship no shell,
// curl or wget — the app binary is the only thing that can run the check.
func Probe(ctx context.Context) error {
	return probeURL(ctx, LocalHealthURL())
}

func probeURL(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build health probe request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health probe request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("health probe: unhealthy status %d", resp.StatusCode)
	}
	return nil
}

// ProbeMain runs Probe with a default timeout and exits the process: status 0
// when healthy, 1 (after writing the reason to stderr) when not. It is intended
// to back a `-healthcheck` CLI flag so a single binary serves both the app and
// its own container healthcheck.
func ProbeMain() {
	// cancel() is called explicitly (not deferred) because os.Exit below skips
	// deferred calls.
	ctx, cancel := context.WithTimeout(context.Background(), defaultProbeTimeout)
	err := Probe(ctx)
	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
}
