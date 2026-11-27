package voice

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stallListener takes TCP connections and never says a word, like an address that accepts TCP but never answers the
// TLS handshake. It counts the connections it took.
func stallListener(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		n     atomic.Int32
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n.Add(1)
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String(), &n
}

// tlsServer is an httptest TLS server that keeps quiet about handshakes a test makes fail on purpose.
func tlsServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// upstreamAt makes example.com resolve to addrs, in that order, trusts srv's certificate (none: the system's pool) and
// shortens the per-address budget; all put back, and the shared client's pool and memory emptied, after the test.
func upstreamAt(t *testing.T, srv *httptest.Server, addrs ...string) {
	t.Helper()
	lookup, roots, addrBudget, lastBudget := lookupUpstream, upstreamRoots, tlsAddrBudget, tlsLastBudget
	t.Cleanup(func() {
		lookupUpstream, upstreamRoots, tlsAddrBudget, tlsLastBudget = lookup, roots, addrBudget, lastBudget
		lastGood.Clear()
		directClient.CloseIdleConnections()
	})
	lookupUpstream = func(_ context.Context, host, _ string) ([]string, error) {
		if host != "example.com" {
			return nil, fmt.Errorf("unexpected lookup of %s", host)
		}
		return addrs, nil
	}
	upstreamRoots = nil
	if srv != nil {
		pool := x509.NewCertPool()
		pool.AddCert(srv.Certificate())
		upstreamRoots = pool
	}
	tlsAddrBudget, tlsLastBudget = 300*time.Millisecond, 5*time.Second
}

func TestDialTLSStopsWhenCanceled(t *testing.T) {
	stall, _ := stallListener(t)
	other, _ := stallListener(t)
	upstreamAt(t, nil, stall, other)
	tlsAddrBudget, tlsLastBudget = 10*time.Second, 10*time.Second

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := dialTLS(ctx, "tcp", "example.com:7004")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("canceled dial returned after %s", took)
	}
}

func TestDialTLSToAnAddressLiteral(t *testing.T) {
	srv := tlsServer(t, http.NotFoundHandler())
	upstreamAt(t, srv) // a lookup would fail the test: an address literal is dialed as it is
	lookupUpstream = func(_ context.Context, host, _ string) ([]string, error) {
		t.Errorf("looked up %s", host)
		return nil, errors.New("no lookup")
	}

	conn, err := dialTLS(context.Background(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
