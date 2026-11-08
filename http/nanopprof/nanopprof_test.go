package nanopprof_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
)

func config(enabled bool) nanopprof.Config {
	return nanopprof.Config{
		Enabled: enabled, Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	}
}

func TestDisabledServerServesNothingAndWaitsForTheContext(t *testing.T) {
	srv := nanopprof.NewServer(config(false), slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	select {
	case err := <-done:
		t.Fatalf("a disabled Run returned early (%v); that would shut the App down", err)
	case <-time.After(50 * time.Millisecond):
	}
	if srv.Addr() != nil {
		t.Fatal("a disabled server listened")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

func TestEnabledServerServesTheIndexAndStops(t *testing.T) {
	srv := nanopprof.NewServer(config(true), slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	select {
	case <-srv.Listening():
	case err := <-done:
		t.Fatalf("Run returned before listening: %v", err)
	}

	resp, err := http.Get("http://" + srv.Addr().String() + "/debug/pprof/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err = <-done; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// A CPU profile in progress when the App stops must not turn the planned
// stop into a failure: Run closes it after ShutdownTimeout and returns nil.
func TestAProfileInProgressDoesNotFailTheStop(t *testing.T) {
	cfg := config(true)
	cfg.ShutdownTimeout = 50 * time.Millisecond
	srv := nanopprof.NewServer(cfg, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	<-srv.Listening()

	requested := make(chan struct{})
	go func() {
		close(requested)
		resp, err := http.Get("http://" + srv.Addr().String() + "/debug/pprof/profile?seconds=3")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-requested
	time.Sleep(100 * time.Millisecond) // the profile request is in flight
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil after a timed-out drain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run waited for the profile instead of ShutdownTimeout")
	}
}
