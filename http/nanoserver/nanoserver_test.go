package nanoserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testConfig() nanoserver.Config {
	return nanoserver.Config{
		Host: "127.0.0.1", ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second,
	}
}

// start runs srv until the returned cancel is called; done receives Run's
// result.
func start(t *testing.T, srv *nanoserver.Server) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- srv.Run(ctx) }()
	select {
	case <-srv.Listening():
	case err := <-result:
		t.Fatalf("Run returned before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not listen")
	}
	t.Cleanup(cancel)
	return cancel, result
}

func TestRunDrainsAnInFlightRequestOnCancel(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	srv := nanoserver.NewServer(testConfig(), handler, discard())
	cancel, done := start(t, srv)

	response := make(chan string, 1)
	go func() {
		resp, err := http.Get(
			"http://" + srv.Addr().String() + "/slow",
		)
		if err != nil {
			response <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		response <- strconv.Itoa(resp.StatusCode) + " " + string(body)
	}()
	<-entered
	cancel()
	// Shutdown closes the listener at once; the in-flight request still ends.
	time.Sleep(50 * time.Millisecond)
	close(release)

	if got := <-response; got != "200 done" {
		t.Fatalf("in-flight response = %q, want 200 done", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run after a clean drain = %v, want nil", err)
	}
	if _, err := net.DialTimeout("tcp", srv.Addr().String(), time.Second); err == nil {
		t.Fatal("the listener still accepts connections after Run returned")
	}
}

func TestRunClosesConnectionsAfterTheShutdownTimeout(t *testing.T) {
	entered := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	})
	cfg := testConfig()
	cfg.ShutdownTimeout = 50 * time.Millisecond
	srv := nanoserver.NewServer(cfg, handler, discard())
	cancel, done := start(t, srv)

	go func() {
		resp, err := http.Get(
			"http://" + srv.Addr().String() + "/stream",
		)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run = %v, want the drain deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ShutdownTimeout")
	}
}

func TestRunFailsWhenThePortIsTaken(t *testing.T) {
	taken, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	cfg := testConfig()
	addr, ok := taken.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %T is not TCP", taken.Addr())
	}
	cfg.Port = addr.Port

	err = nanoserver.NewServer(cfg, http.NotFoundHandler(), discard()).Run(t.Context())

	if err == nil {
		t.Fatal("Run succeeded on a taken port; the App must stop instead")
	}
}

func TestServerRunsUnderTheApp(t *testing.T) {
	srv := nanoserver.NewServer(testConfig(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), discard())
	app := nano.NewApp("http-test", nano.AppConfig{})
	app.Manage("httpserver", srv)
	app.SetLogger(discard())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-srv.Listening()

	resp, err := http.Get("http://" + srv.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatalf("App.Run = %v, want nil after the signal", err)
	}
}
