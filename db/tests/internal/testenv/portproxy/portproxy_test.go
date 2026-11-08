package portproxy

import (
	"bufio"
	"fmt"
	"net"
	"testing"
)

// echoServer answers every line with its name, so a client can tell which target it reached.
func echoServer(t *testing.T, name string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					if _, err = r.ReadString('\n'); err != nil {
						return
					}
					fmt.Fprintln(conn, name)
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func ask(t *testing.T, port int) (string, error) {
	t.Helper()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err = fmt.Fprintln(conn, "hi"); err != nil {
		return "", err
	}
	return bufio.NewReader(conn).ReadString('\n')
}

func TestSetTargetSwitchesNewConnections(t *testing.T) {
	p, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	if got, askErr := ask(t, p.Port()); askErr == nil {
		t.Fatalf("no target set, but the connection answered %q", got)
	}

	p.SetTarget(echoServer(t, "first"))
	if got, askErr := ask(t, p.Port()); askErr != nil || got != "first\n" {
		t.Fatalf("first target: %q, %v", got, askErr)
	}

	p.SetTarget(echoServer(t, "second"))
	if got, askErr := ask(t, p.Port()); askErr != nil || got != "second\n" {
		t.Fatalf("after SetTarget: %q, %v", got, askErr)
	}
}
