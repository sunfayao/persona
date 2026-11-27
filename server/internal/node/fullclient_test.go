//go:build !windows

package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/herdr"
	"github.com/sunfayao/persona/server/internal/rpc"
)

const fullTestSocket = "/x/lab/herdr.sock"

// fakeFullHerdrBin stands in for herdr: `session list` names the session the node's socket belongs to, `session attach`
// runs tail in the PTY after saying its pid.
func fakeFullHerdrBin(t *testing.T, tail string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
case "$1 $2" in
"session list") printf '{"sessions":[{"name":"lab","default":true,"socket_path":"` + fullTestSocket + `"}]}' ;;
"session attach")
  echo "pid $$"
` + tail + `
  ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

const (
	fullIdle   = `  while :; do sleep 0.05; done`
	fullFlood  = `  exec yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	fullSizing = `  last=""; while :; do s=$(stty size); if [ "$s" != "$last" ]; then echo "size $s"; last=$s; fi; sleep 0.05; done`
)

// fullHub plays the hub: it records what the node sends about full clients.
type fullHub struct {
	n  *Node
	mu sync.Mutex
	// data by client id, the raw bytes; ends by client id
	data  map[string][]byte
	chunk map[string]int
	ends  map[string]string
}

func newFullHub(t *testing.T, tail string) *fullHub {
	t.Helper()
	n := &Node{agents: nil, state: newState(), hosts: herdrHosts(&herdr.Client{Bin: fakeFullHerdrBin(t, tail), Socket: fullTestSocket})}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	f := &fullHub{n: n, data: map[string][]byte{}, chunk: map[string]int{}, ends: map[string]string{}}
	hub.OnEvent = func(event string, params json.RawMessage) {
		var p struct {
			ClientID string `json:"client_id"`
			Data     string `json:"data"`
			Error    string `json:"error"`
		}
		_ = json.Unmarshal(params, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch event {
		case "herdr.client.data":
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			f.data[p.ClientID] = append(f.data[p.ClientID], b...)
			f.chunk[p.ClientID]++
		case "herdr.client.end":
			f.ends[p.ClientID] = p.Error
		}
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { n.closeFulls(); cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return f
}

func (f *fullHub) open(id string, cols, rows int) error {
	_, err := f.n.fullOpen(context.Background(), json.RawMessage(fmt.Sprintf(`{"client_id":%q,"cols":%d,"rows":%d}`, id, cols, rows)))
	return err
}

func (f *fullHub) event(name, id string, extra string) {
	params := fmt.Sprintf(`{"client_id":%q%s}`, id, extra)
	f.n.onFullEvent(name, json.RawMessage(params))
}

func (f *fullHub) bytes(id string) int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.data[id]) }
func (f *fullHub) text(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.data[id])
}
func (f *fullHub) end(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.ends[id]
	return s, ok
}

func (f *fullHub) waitText(t *testing.T, id, want string) {
	t.Helper()
	fullWait(t, 10*time.Second, fmt.Sprintf("%q from %s (got %q)", want, id, f.text(id)), func() bool { return strings.Contains(f.text(id), want) })
}

func fullWait(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fullHub) pid(t *testing.T, id string) int {
	t.Helper()
	f.waitText(t, id, "pid ")
	fullWait(t, 5*time.Second, "the pid line", func() bool {
		return strings.Contains(f.text(id), "pid ") && strings.Contains(f.text(id)[strings.Index(f.text(id), "pid "):], "\n")
	})
	s := f.text(id)
	s = s[strings.Index(s, "pid ")+4:]
	n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(s, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("pid in %q: %v", s, err)
	}
	return n
}

func gone(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

func TestFullClientEventsForOtherClientsAreIgnored(t *testing.T) {
	h := newFullHub(t, fullIdle)
	if !h.n.onFullEvent("herdr.client.input", json.RawMessage(`{"client_id":"nobody","data":"x"}`)) {
		t.Fatal("a full client event was not recognised")
	}
	if h.n.onFullEvent("terminal.input", json.RawMessage(`{}`)) {
		t.Fatal("a terminal event was taken as a full client event")
	}
}

func TestFullClientWithoutAHerdrSessionIsUnsupported(t *testing.T) {
	n := &Node{state: newState(), hosts: herdrHosts(&herdr.Client{Bin: fakeFullHerdrBin(t, fullIdle), Socket: "/not/a/session.sock"})}
	_, err := n.fullOpen(context.Background(), json.RawMessage(`{"client_id":"c","cols":80,"rows":24}`))
	if err == nil || !strings.HasPrefix(err.Error(), "unsupported") {
		t.Fatalf("open = %v", err)
	}
}
