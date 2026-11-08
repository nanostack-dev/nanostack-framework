package nanopprof

import (
	"log/slog"
	"testing"
	"time"
)

// A Config built by hand with only Enabled must not listen on every
// interface, nor without timeouts.
func TestAZeroConfigListensOnLoopbackWithTimeouts(t *testing.T) {
	srv := NewServer(Config{Enabled: true}, slog.New(slog.DiscardHandler))
	if srv.server.Addr != "127.0.0.1:6060" {
		t.Fatalf("Addr = %q, want 127.0.0.1:6060", srv.server.Addr)
	}
	got := []time.Duration{
		srv.server.ReadHeaderTimeout, srv.server.ReadTimeout, srv.server.WriteTimeout,
		srv.server.IdleTimeout, srv.shutdownTimeout,
	}
	want := []time.Duration{30 * time.Second, 60 * time.Second, 60 * time.Second, 120 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("timeout %d = %v, want %v", i, got[i], want[i])
		}
	}
}
