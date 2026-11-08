// Package portproxy forwards a local TCP port, fixed for the life of the process, to a
// target address that can change between suite runs.
package portproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
)

// Proxy accepts connections on a loopback port and pipes each one to the current target.
type Proxy struct {
	port int

	mu     sync.RWMutex
	target string
}

// Listen reserves a free loopback port and starts serving it. Connections made before
// a target is set are closed.
func Listen() (*Proxy, error) {
	var lc net.ListenConfig
	v4, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("portproxy: %w", err)
	}
	addr, ok := v4.Addr().(*net.TCPAddr)
	if !ok {
		_ = v4.Close()
		return nil, fmt.Errorf("portproxy: %s is not a TCP address", v4.Addr())
	}
	p := &Proxy{port: addr.Port}
	go p.serve(v4)

	// Clients dialling "localhost" may try ::1 first; serve it too when the port is free there.
	if v6, v6Err := lc.Listen(context.Background(), "tcp", fmt.Sprintf("[::1]:%d", p.port)); v6Err == nil {
		go p.serve(v6)
	}
	return p, nil
}

// Port returns the local port.
func (p *Proxy) Port() int { return p.port }

// SetTarget makes new connections go to addr (host:port).
func (p *Proxy) SetTarget(addr string) {
	p.mu.Lock()
	p.target = addr
	p.mu.Unlock()
}

func (p *Proxy) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go p.pipe(conn)
	}
}

func (p *Proxy) pipe(client net.Conn) {
	defer client.Close()

	p.mu.RLock()
	target := p.target
	p.mu.RUnlock()
	if target == "" {
		return
	}

	var d net.Dialer
	server, err := d.DialContext(context.Background(), "tcp", target)
	if err != nil {
		return
	}
	defer server.Close()

	// When either side ends, close both so the other copy returns too.
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = server.Close()
		})
	}
	go func() {
		_, _ = io.Copy(server, client)
		closeBoth()
	}()
	_, _ = io.Copy(client, server)
	closeBoth()
}
