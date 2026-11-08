package nanoserver

import (
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestAZeroConfigGetsEveryTimeout(t *testing.T) {
	srv := NewServer(Config{}, http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	got := map[string]time.Duration{
		"ReadHeaderTimeout": srv.server.ReadHeaderTimeout,
		"ReadTimeout":       srv.server.ReadTimeout,
		"WriteTimeout":      srv.server.WriteTimeout,
		"IdleTimeout":       srv.server.IdleTimeout,
		"ShutdownTimeout":   srv.shutdownTimeout,
	}
	want := map[string]time.Duration{
		"ReadHeaderTimeout": DefaultReadHeaderTimeout,
		"ReadTimeout":       DefaultReadTimeout,
		"WriteTimeout":      DefaultWriteTimeout,
		"IdleTimeout":       DefaultIdleTimeout,
		"ShutdownTimeout":   DefaultShutdownTimeout,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %v, want %v", name, got[name], w)
		}
	}
}

func TestASetTimeoutIsKept(t *testing.T) {
	srv := NewServer(Config{ReadHeaderTimeout: time.Second, ShutdownTimeout: 2 * time.Second},
		http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	if srv.server.ReadHeaderTimeout != time.Second || srv.shutdownTimeout != 2*time.Second {
		t.Fatalf("got %v and %v, want 1s and 2s", srv.server.ReadHeaderTimeout, srv.shutdownTimeout)
	}
}
