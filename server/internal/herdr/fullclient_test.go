//go:build !windows

package herdr

import (
	"strings"
	"testing"
)

func TestFullClientNeedsASessionAndASize(t *testing.T) {
	c := &Client{Bin: "herdr"}
	for _, o := range []FullOptions{{Cols: 80, Rows: 24}, {Session: "lab", Rows: 24}, {Session: "lab", Cols: 80}} {
		if _, err := c.StartFull(o, func([]byte) {}); err == nil {
			t.Fatalf("started with %+v", o)
		}
	}
}

func TestSessionNameIn(t *testing.T) {
	list := []byte(`{"sessions":[{"name":"default","default":true,"socket_path":"/home/u/.config/herdr/herdr.sock"},` +
		`{"name":"lab","default":false,"socket_path":"/home/u/.config/herdr/sessions/lab/herdr.sock"}]}`)
	for _, c := range []struct{ socket, want string }{
		{"/home/u/.config/herdr/herdr.sock", "default"},
		{"/home/u/.config/herdr/sessions/lab/herdr.sock", "lab"},
		{"/home/u/.config/herdr/sessions/lab/../lab/herdr.sock", "lab"},
		{"", "default"},
		{"/somewhere/else.sock", ""},
	} {
		got, err := sessionNameIn(list, c.socket)
		if c.want == "" {
			if err == nil || !strings.HasPrefix(err.Error(), "unsupported") {
				t.Fatalf("%q: %q, %v", c.socket, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("%q: %q, %v; want %q", c.socket, got, err, c.want)
		}
	}
	if _, err := sessionNameIn([]byte("not json"), ""); err == nil {
		t.Fatal("read a list that is not JSON")
	}
}

func TestFullClientEnv(t *testing.T) {
	got := fullClientEnv([]string{"PATH=/bin", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH=/x", "TMUX=/t", "TERM=dumb", "SSH_CONNECTION=1 2 3 4", "HOME=/h"})
	want := map[string]bool{"PATH=/bin": true, "HOME=/h": true, "TERM=xterm-256color": true, "COLORTERM=truecolor": true, "SSH_CONNECTION=127.0.0.1 1 127.0.0.1 2": true}
	if len(got) != len(want) {
		t.Fatalf("env = %v", got)
	}
	for _, kv := range got {
		if !want[kv] {
			t.Fatalf("unexpected %q in %v", kv, got)
		}
	}
}
