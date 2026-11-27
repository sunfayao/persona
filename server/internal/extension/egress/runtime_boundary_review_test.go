package egress

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeBusyRejectDoesNotDrainBody(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{})
	defer p.Close()
	p.runtimeActive = map[string]int{"a": 4}
	p.runtimeTotal = 4
	socket := filepath.Join(shortTempDir(t), "proxy.sock")
	if err := p.ListenRuntimeUnix("a", socket); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(300 * time.Millisecond))
	_, _ = io.WriteString(c, "POST "+RuntimeFetchPath+" HTTP/1.1\r\nHost: broker\r\nContent-Length: 1000\r\n\r\nx")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal("busy rejection drained incomplete body", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || !resp.Close {
		t.Fatal(resp.StatusCode, resp.Close)
	}
}

func TestRuntimeCancelInterruptsPendingUpstreamCONNECT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	proxy, _ := url.Parse("http://" + ln.Addr().String())
	p := &Proxy{Upstream: func(*url.URL) (*url.URL, error) { return proxy, nil }, Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, err := p.runtimeDial(ctx, "api.example.com", "443", false)
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	var c net.Conn
	select {
	case c = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("proxy was not dialed")
	}
	defer c.Close()
	// The trusted proxy receives the request but never answers CONNECT.
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	r, err := http.ReadRequest(bufio.NewReader(c))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled CONNECT succeeded")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled CONNECT retained its socket")
	}
}
