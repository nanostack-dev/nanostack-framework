package redistest

import (
	"net"
	"sync"
	"testing"
)

// ClosedAddr returns a local address where nothing listens, for a Redis
// that is down.
func ClosedAddr(t testing.TB) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("redistest: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

// SilentServer returns the address of a server that accepts connections and
// never answers, for a Redis that hangs. It closes them when the test ends.
func SilentServer(t testing.TB) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("redistest: %v", err)
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
