package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// residentAttach is a resident node (Attach) and a manager's peer framed with it over a first transport, resuming
// on: what these tests take back, refuse or load.
type residentAttach struct {
	t     *testing.T
	home  string
	ctx   context.Context
	extra chan net.Conn
	made  atomic.Int32
	done  chan error
	hub   *rpc.Peer
	first net.Conn // the manager's end of the first transport
	lost  chan error
	stop  context.CancelFunc
	hooks hookEvents // the hook.event notifications the manager got
}

func startResidentAttach(t *testing.T) *residentAttach {
	home := shortHome(t)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &residentAttach{t: t, home: home, ctx: ctx, extra: make(chan net.Conn), done: make(chan error, 1),
		lost: make(chan error, 64), stop: cancel}
	go func() {
		r.done <- Attach(ctx, func() (*Node, error) {
			r.made.Add(1)
			n, err := New("test")
			if err == nil {
				n.logger.SetOutput(io.Discard)
				t.Cleanup(n.starts.close)
			}
			return n, err
		}, r.extra, nil)
	}()
	m1, n1 := net.Pipe()
	r.extra <- n1
	r.first = m1
	hub := rpc.New("h", m1)
	hub.EnableLanes(nodeproto.ControlLane, nodeproto.BulkLane)
	hub.EnableResume(5 * time.Second)
	hub.OnLost = func(err error) { r.lost <- err }
	hub.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }
	hub.OnEvent = r.hooks.record
	go func() { _ = hub.Serve(ctx, slowReader{m1}) }()
	r.hub = hub
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	var info proto.NodeInfo
	if err := hub.Call(cctx, "node.info", map[string]any{}, &info); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(info.Capabilities, nodeproto.NodeResume) {
		t.Fatalf("a resident node does not announce %s", nodeproto.NodeResume)
	}
	if err := hub.UpgradeLanes(cctx); err != nil {
		t.Fatal(err)
	}
	if !hub.Resumable() {
		t.Fatal("not resumable after rpc.lanes with a resident node")
	}
	return r
}

// end stops Attach and waits for it.
func (r *residentAttach) end() {
	r.t.Helper()
	r.stop()
	select {
	case <-r.done:
	case <-time.After(15 * time.Second):
		r.t.Fatal("Attach did not end")
	}
}

// load keeps the node writing: listings of a large folder (answers no credit holds back) asked over and over by a
// few callers until stop is called, which says the first error any of them had. more waits for another answer.
func (r *residentAttach) load(ctx context.Context) (stop func() error, more func()) {
	r.t.Helper()
	dir := filepath.Join(r.home, "load")
	if err := os.Mkdir(dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	for i := range 400 {
		name := fmt.Sprintf("%03d-%s.txt", i, strings.Repeat("persona", 20))
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			r.t.Fatal(err)
		}
	}
	lctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	var answers atomic.Int64
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for lctx.Err() == nil {
				var list json.RawMessage
				err := r.hub.Call(lctx, "fs.list", map[string]any{"path": dir}, &list)
				if lctx.Err() != nil {
					return
				}
				if err != nil || len(list) < 400*100 {
					errs <- fmt.Errorf("listing: %d bytes, %v", len(list), err)
					return
				}
				answers.Add(1)
			}
		}()
	}
	stop = func() error {
		cancel()
		wg.Wait()
		close(errs)
		if answers.Load() == 0 {
			return errors.New("no answer came")
		}
		return <-errs
	}
	more = func() {
		r.t.Helper()
		was := answers.Load()
		for deadline := time.Now().Add(20 * time.Second); answers.Load() == was; {
			if time.Now().After(deadline) {
				r.t.Fatal("no answer came")
			}
			time.Sleep(time.Millisecond) // sleep-ok: polls a counter the callers bump
		}
	}
	return stop, more
}

// slowReader is the manager's end of a slow link: what the node writes waits on it.
type slowReader struct{ r io.Reader }

func (s slowReader) Read(b []byte) (int, error) {
	time.Sleep(time.Millisecond) // sleep-ok: at most 16 KiB a millisecond
	return s.r.Read(b[:min(len(b), 16<<10)])
}

func TestAttachResumesAConnection(t *testing.T) {
	r := startResidentAttach(t)
	hub := r.hub
	cctx, ccancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer ccancel()
	r.first.Close() // the transport goes: a relay ended, a link dropped
	select {
	case <-r.lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's peer was not told its transport was lost")
	}
	m2, n2 := net.Pipe()
	r.extra <- n2
	if err := hub.Resume(cctx, m2, m2); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := hub.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatalf("after resuming: %v", err)
	}
	if r.made.Load() != 1 {
		t.Fatalf("%d Nodes made: a resume made a new one", r.made.Load())
	}

	// A token this node does not have: refused at once, the connection served left alone.
	if line := refusedResume(t, r); !strings.Contains(line, rpc.CodeResumeRefused) {
		t.Fatalf("a made-up token: %q", line)
	}
	if err := hub.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatalf("after a refused resume: %v", err)
	}
	if r.made.Load() != 1 {
		t.Fatalf("%d Nodes made: a refused resume made one", r.made.Load())
	}
	r.end()
}

// refusedResume asks the resident node to resume with a token it does not have, on a new transport, and says the line
// it answered.
func refusedResume(t *testing.T, r *residentAttach) string {
	t.Helper()
	m, n := net.Pipe()
	defer m.Close()
	r.extra <- n
	go func() {
		_, _ = m.Write([]byte(`{"id":"x1","method":"rpc.resume","params":{"token":"not-this-one","read":0}}` + "\n"))
	}()
	_ = m.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(m).ReadString('\n')
	if err != nil {
		t.Fatalf("a made-up token: %q, %v", line, err)
	}
	return line
}

// A transport that ends before it said a whole first line (a manager's attempt to resume cut on its way) is let go:
// it does not replace the connection kept (#10829's review).
func TestAttachIgnoresATransportThatSaidNothing(t *testing.T) {
	home := shortHome(t)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var made atomic.Int32
	extra := make(chan net.Conn)
	done := make(chan error, 1)
	go func() {
		done <- Attach(ctx, func() (*Node, error) {
			made.Add(1)
			n, err := New("test")
			if err == nil {
				n.logger.SetOutput(io.Discard)
				t.Cleanup(n.starts.close)
			}
			return n, err
		}, extra, nil)
	}()
	m1, n1 := net.Pipe()
	extra <- n1
	hub := rpc.New("h", m1)
	hub.EnableLanes(nodeproto.ControlLane, nodeproto.BulkLane)
	hub.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }
	go func() { _ = hub.Serve(ctx, m1) }()
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	if err := hub.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, partial := range []string{"", `{"id":"h7","method":"rpc.res`} {
		m2, n2 := net.Pipe()
		extra <- n2
		go func() {
			_, _ = m2.Write([]byte(partial))
			m2.Close()
		}()
	}
	if err := hub.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatalf("the connection served after two that said nothing: %v", err)
	}
	if made.Load() != 1 {
		t.Fatalf("%d Nodes made", made.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Attach did not end")
	}
}

// Connections replace each other in the order they came, whatever order they speak in: one that came before the
// connection served and speaks only now (an attempt the manager gave up on) is let go, not served in its place
// (#10829's review).
func TestAttachKeepsTheOrderConnectionsCameIn(t *testing.T) {
	home := shortHome(t)
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var made atomic.Int32
	extra := make(chan net.Conn)
	done := make(chan error, 1)
	go func() {
		done <- Attach(ctx, func() (*Node, error) {
			made.Add(1)
			n, err := New("test")
			if err == nil {
				n.logger.SetOutput(io.Discard)
				t.Cleanup(n.starts.close)
			}
			return n, err
		}, extra, nil)
	}()
	manager := func(name string, c net.Conn) *rpc.Peer {
		p := rpc.New(name, c)
		p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }
		go func() { _ = p.Serve(ctx, c) }()
		return p
	}
	m1, n1 := net.Pipe()
	extra <- n1 // came first, says nothing yet
	m2, n2 := net.Pipe()
	extra <- n2
	newer := manager("h2", m2)
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	if err := newer.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	older := manager("h1", m1)
	octx, ocancel := context.WithTimeout(ctx, 3*time.Second)
	defer ocancel()
	if err := older.Call(octx, "ping", map[string]any{}, nil); err == nil {
		t.Fatal("the connection that came first was served after the newer one")
	}
	if err := newer.Call(cctx, "ping", map[string]any{}, nil); err != nil {
		t.Fatalf("the newer connection: %v", err)
	}
	if made.Load() != 1 {
		t.Fatalf("%d Nodes made", made.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Attach did not end")
	}
}
