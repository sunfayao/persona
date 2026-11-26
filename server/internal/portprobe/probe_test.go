package portprobe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPageTitle(t *testing.T) {
	t.Parallel()
	for page, want := range map[string]string{
		`<html><head><title>Vite + React</title></head>`: "Vite + React",
		"<TITLE lang=en>\n  My &amp; App\n</TITLE>":      "My & App",
		`<html><body>no title</body>`:                    "",
		"<title>" + strings.Repeat("长", 80) + "</title>": strings.Repeat("长", 60) + "…",
	} {
		if got := pageTitle(page); got != want {
			t.Errorf("pageTitle(%.30q) = %q, want %q", page, got, want)
		}
	}
}

// The probe: an HTML page gives its title, a JSON API is HTTP without one, a socket speaking something else is
// not listed, one that accepts and says nothing yet is pending, and a closed port is nothing.
func TestProbePort(t *testing.T) {
	t.Parallel()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "localhost:") {
			http.Error(w, "blocked host "+r.Host, http.StatusForbidden) // vite's host check
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>Dev page</title>")
	}))
	defer page.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer api.Close()
	other := rawServer(t, func(c net.Conn) { _, _ = c.Write([]byte("SSH-2.0-OpenSSH_9\r\n")) })
	silent := rawServer(t, func(c net.Conn) { _, _ = io.Copy(io.Discard, c) })

	ctx := context.Background()
	if got := Probe(ctx, portOf(t, page.URL), "127.0.0.1", time.Now); !got.HTTP || got.Title != "Dev page" {
		t.Errorf("page: %+v", got)
	}
	if got := Probe(ctx, portOf(t, api.URL), "127.0.0.1", time.Now); !got.HTTP || got.Title != "" {
		t.Errorf("api: %+v", got)
	}
	if got := Probe(ctx, other, "0.0.0.0", time.Now); got.HTTP {
		t.Errorf("ssh: %+v", got)
	}
	if got := Probe(ctx, silent, "127.0.0.1", time.Now); !got.HTTP || !got.Pending {
		t.Errorf("silent: %+v", got)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if got := Probe(ctx, closed, "", time.Now); got.HTTP {
		t.Errorf("closed: %+v", got)
	}
}

func rawServer(t *testing.T, serve func(net.Conn)) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); serve(c) }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func portOf(t *testing.T, u string) int {
	t.Helper()
	i := strings.LastIndexByte(u, ':')
	p, err := strconv.Atoi(u[i+1:])
	if err != nil {
		t.Fatal(err)
	}
	return p
}
