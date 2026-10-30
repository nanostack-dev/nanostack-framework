package redistest

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
)

// Faults breaks the connections of one go-redis client on purpose. The server
// stays healthy; only this client sees the faults.
type Faults struct {
	mu   sync.Mutex
	lose []string
	lost int
}

// FaultyClient returns a go-redis client to addr whose connections obey the
// returned Faults. configure, when given, changes the client options, for
// example MaxRetries. The client is closed when the test ends.
func FaultyClient(t testing.TB, addr string, configure ...func(*redis.Options)) (*redis.Client, *Faults) {
	t.Helper()
	faults := &Faults{}
	var dialer net.Dialer
	opts := &redis.Options{
		Addr: addr,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &faultyConn{Conn: conn, faults: faults}, nil
		},
	}
	for _, change := range configure {
		change(opts)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, faults
}

// LoseReply makes the next command called command reach the server and run,
// while its reply never reaches the client: the connection closes first, as
// when the network fails at that moment. go-redis then sends the command again
// on a new connection, unless MaxRetries is negative. Call it once for each
// reply to lose.
func (f *Faults) LoseReply(command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lose = append(f.lose, strings.ToLower(command))
}

// Lost returns how many replies were lost so far.
func (f *Faults) Lost() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lost
}

// doom reports whether the command in written is the next one to lose.
func (f *Faults) doom(written []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.lose) == 0 {
		return false
	}
	name := f.lose[0]
	// A command is an array of bulk strings: *<n>\r\n$<len>\r\n<name>\r\n...
	head := "$" + strconv.Itoa(len(name)) + "\r\n" + name + "\r\n"
	if !bytes.Contains(bytes.ToLower(written), []byte(head)) {
		return false
	}
	f.lose = f.lose[1:]
	f.lost++
	return true
}

type faultyConn struct {
	net.Conn
	faults *Faults
	doomed bool
}

func (c *faultyConn) Write(b []byte) (int, error) {
	if c.faults.doom(b) {
		c.doomed = true
	}
	return c.Conn.Write(b)
}

func (c *faultyConn) Read(b []byte) (int, error) {
	if !c.doomed {
		return c.Conn.Read(b)
	}
	// Wait for the reply, which proves the server ran the command, then drop
	// it with the connection.
	_, _ = c.Conn.Read(b)
	_ = c.Conn.Close()
	return 0, io.ErrUnexpectedEOF
}
