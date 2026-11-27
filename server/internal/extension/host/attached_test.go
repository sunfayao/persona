package host

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extension/serve"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// far is a plugin running on its own on a unix socket: it serves demo() on each connection, or closes it at once
// (mute), and counts the connections it took.
type far struct {
	ln      net.Listener
	accepts atomic.Int32
	mu      sync.Mutex
	conns   []net.Conn
}

func serveFar(t *testing.T, sock string, mute bool) *far {
	if mute {
		return serveFarAs(t, sock, nil)
	}
	return serveFarAs(t, sock, demo)
}

// serveFarAs is serveFar with the program each connection gets (nil: closed at once).
func serveFarAs(t *testing.T, sock string, program func() serve.Program) *far {
	if program == nil {
		return serveFarWith(t, sock, nil)
	}
	return serveFarWith(t, sock, func(c net.Conn) { _ = serve.Serve(context.Background(), c, c, program()) })
}

// serveFarWith is serveFar with what serves each connection (nil: closed at once).
func serveFarWith(t *testing.T, sock string, handle func(net.Conn)) *far {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	f := &far{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.accepts.Add(1)
			if handle == nil {
				c.Close()
				continue
			}
			f.mu.Lock()
			f.conns = append(f.conns, c)
			f.mu.Unlock()
			go func() { defer c.Close(); handle(c) }()
		}
	}()
	t.Cleanup(f.stop)
	return f
}

// stop is the program ending: its socket and every connection close.
func (f *far) stop() {
	f.ln.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Close()
	}
	f.conns = nil
}

func farDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func attachedSpec(t *testing.T, sock string, o Observer) Spec {
	s := spec(t, "")
	s.Command, s.RPCSocket, s.Attached, s.Startup, s.Observe = "", sock, true, true, o
	return s
}

func timings(t *testing.T, retry, grace time.Duration) {
	r, g := RemoteRetry, OfflineGrace
	RemoteRetry, OfflineGrace = retry, grace
	t.Cleanup(func() { RemoteRetry, OfflineGrace = r, g })
}

// A call while it is offline within the grace waits for it, and works once the program is back.
func TestAttachedCallWaitsWithinGrace(t *testing.T) {
	timings(t, 20*time.Millisecond, time.Hour)
	sock := filepath.Join(farDir(t), "far.sock")
	f := serveFar(t, sock, false)
	p := Start(context.Background(), attachedSpec(t, sock, nil))
	defer p.Stop()
	waitState(t, p, StateRunning)
	f.stop()
	waitState(t, p, StateStarting)
	var n int
	done := make(chan error, 1)
	go func() { done <- p.Call(context.Background(), "demo.pid", struct{}{}, &n) }()
	select {
	case err := <-done:
		t.Fatalf("the call did not wait for the program: %v", err)
	case <-time.After(200 * time.Millisecond): // ten tries
	}
	serveFar(t, sock, false)
	select {
	case err := <-done:
		if err != nil || n != os.Getpid() {
			t.Fatalf("%v (%d)", err, n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the call never came back")
	}
}

// One that answers as another program (another id) is refused for that, and the version it says is not this plugin's.
func TestAttachedOtherProgramSaysNoVersion(t *testing.T) {
	timings(t, 10*time.Millisecond, time.Hour)
	sock := filepath.Join(farDir(t), "far.sock")
	serveFar(t, sock, false)
	s := attachedSpec(t, sock, nil)
	init := s.Init
	s.Init = func(ctx context.Context) (params extension.InitializeParams, err error) {
		params, err = init(ctx)
		params.Extension.ID, params.Extension.Version = "other", "2.0.0"
		return params, err
	}
	p := Start(context.Background(), s)
	defer p.Stop()
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, `"other"`) {
		t.Fatal(why)
	}
	if l := p.Link(); l.Said != "" {
		t.Fatalf("link %+v", l)
	}
}

// One that answers as this extension but in another protocol is refused for that, and the version it says, in a
// protocol this host does not speak, is not recorded.
func TestAttachedOtherProtocolSaysNoVersion(t *testing.T) {
	timings(t, 10*time.Millisecond, time.Hour)
	sock := filepath.Join(farDir(t), "far.sock")
	serveFarWith(t, sock, func(c net.Conn) {
		peer := rpc.New("p", c)
		peer.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			return extension.InitializeResult{Protocol: extension.Protocol + 1, ID: "demo", Version: "1.1.0"}, nil
		}
		_ = peer.Serve(context.Background(), c)
	})
	p := Start(context.Background(), attachedSpec(t, sock, nil))
	defer p.Stop()
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, "协议版本") {
		t.Fatal(why)
	}
	if l := p.Link(); l.Said != "" {
		t.Fatalf("link %+v", l)
	}
}
