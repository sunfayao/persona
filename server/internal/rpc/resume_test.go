package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/netshape"
)

// resumeLink is a manager peer h and a node peer n, framed and resumable over a shaped link that a test cuts (both
// ends closed, what was on its way lost) and resumes over a new one.
type resumeLink struct {
	t            *testing.T
	link         netshape.Config
	h, n         *Peer
	hLost, nLost chan error
	mu           sync.Mutex
	a, b         net.Conn
	served       chan error
}

// newResumeLink makes the pair; setup, when set, configures both peers (handlers) before they serve. hResume and
// nResume enable resuming on each side with window (0: not).
func newResumeLink(t *testing.T, link netshape.Config, window time.Duration, hResume, nResume bool, setup func(h, n *Peer)) *resumeLink {
	t.Helper()
	l := &resumeLink{t: t, link: link, hLost: make(chan error, 8), nLost: make(chan error, 8), served: make(chan error, 2)}
	l.a, l.b = silentPipe(link)
	l.h, l.n = New("h", l.a), New("n", l.b)
	for _, side := range []struct {
		p      *Peer
		resume bool
		lost   chan error
	}{{l.h, hResume, l.hLost}, {l.n, nResume, l.nLost}} {
		side.p.EnableLanes([]string{"ping"}, []string{"bulk", "bulk.event"})
		if side.resume {
			side.p.EnableResume(window)
		}
		lost := side.lost
		side.p.OnLost = func(err error) { lost <- err }
		side.p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }
	}
	if setup != nil {
		setup(l.h, l.n)
	}
	go func() { l.served <- l.h.Serve(context.Background(), l.a) }()
	go func() { l.served <- l.n.Serve(context.Background(), l.b) }()
	t.Cleanup(func() {
		l.h.Close(ErrClosed)
		l.n.Close(ErrClosed)
		l.mu.Lock()
		defer l.mu.Unlock()
		l.a.Close()
		l.b.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.h.UpgradeLanes(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return l.n.Framed() })
	if hResume && nResume { // both inputs framed too: a cut before the node read the marker line ends the connection
		eventually(t, func() bool { return l.h.readsResumable() && l.n.readsResumable() })
	}
	return l
}

// readsResumable: this side's input is framed and read through a transport a resume can replace.
func (p *Peer) readsResumable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resume != nil && p.resume.sr != nil
}

// silentConn is a transport that can go silent without a word (silence): what it is given goes nowhere, nothing comes,
// and neither end is told, as when a computer's address changes.
type silentConn struct {
	net.Conn
	silent atomic.Bool
	gone   chan struct{}
	once   sync.Once
}

func silentPipe(link netshape.Config) (net.Conn, net.Conn) {
	a, b := netshape.Pipe(link, link)
	return &silentConn{Conn: a, gone: make(chan struct{})}, &silentConn{Conn: b, gone: make(chan struct{})}
}

func (c *silentConn) Read(b []byte) (int, error) {
	n, err := 0, error(nil)
	if !c.silent.Load() {
		n, err = c.Conn.Read(b)
	}
	if c.silent.Load() {
		<-c.gone
		return 0, net.ErrClosed
	}
	return n, err
}

func (c *silentConn) Write(b []byte) (int, error) {
	if c.silent.Load() {
		return len(b), nil
	}
	return c.Conn.Write(b)
}

func (c *silentConn) Close() error {
	c.once.Do(func() { close(c.gone) })
	return c.Conn.Close()
}

// silence makes the transport go silent both ways.
func (l *resumeLink) silence() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.a.(*silentConn).silent.Store(true)
	l.b.(*silentConn).silent.Store(true)
}

// cut closes both ends of the transport: what was on its way either way is lost.
func (l *resumeLink) cut() {
	l.mu.Lock()
	a, b := l.a, l.b
	l.mu.Unlock()
	a.Close()
	b.Close()
}

// waitLost waits until both sides were told the transport was lost.
func (l *resumeLink) waitLost() {
	l.t.Helper()
	for _, ch := range []chan error{l.hLost, l.nLost} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			l.t.Fatal("a side was not told its transport was lost")
		}
	}
}

// resume connects the two again over a new link: h asks with its token, n answers as a resident node does, from the
// first line of the new transport.
func (l *resumeLink) resume() error {
	a, b := silentPipe(l.link)
	accepted := make(chan error, 1)
	go func() {
		line, rest, err := ReadFirstLine(b, resumeLineMax)
		if err != nil {
			accepted <- err
			return
		}
		req, ok := ParseResume(line)
		if !ok {
			accepted <- fmt.Errorf("not an rpc.resume: %q", line)
			return
		}
		accepted <- l.n.AcceptResume(req, rest, b)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := l.h.Resume(ctx, a, a)
	if aerr := <-accepted; err == nil {
		err = aerr
	}
	if err != nil {
		a.Close()
		b.Close()
		return err
	}
	l.mu.Lock()
	l.a, l.b = a, b
	l.mu.Unlock()
	return nil
}

// Not resumed within the window: the peer closes as a connection always did, its calls dropped.
func TestResumeWindowEnds(t *testing.T) {
	l := newResumeLink(t, netshape.Config{}, 200*time.Millisecond, true, true, func(h, n *Peer) {
		n.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
	})
	call := make(chan error, 1)
	go func() { call <- l.h.Call(context.Background(), "wait", nil, nil) }()
	eventually(t, func() bool { return len(l.h.pendingMethods()) == 1 })
	l.cut()
	l.waitLost()
	select {
	case err := <-call:
		if !errors.Is(err, ErrDropped) {
			t.Fatalf("the call: %v, want dropped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call still waits after the window")
	}
	if !strings.Contains(fmt.Sprint(l.h.Err()), "not resumed within") {
		t.Fatalf("closed with %v", l.h.Err())
	}
	if err := l.resume(); err == nil {
		t.Fatal("resumed after the window")
	}
}

// A peer that never enabled resuming answers rpc.lanes as before: nothing is kept, and a cut ends the connection.
func TestResumeNeedsBothSides(t *testing.T) {
	for _, c := range []struct{ h, n bool }{{true, false}, {false, true}} {
		l := newResumeLink(t, netshape.Config{}, time.Second, c.h, c.n, nil)
		if l.h.Resumable() || l.n.Resumable() {
			t.Fatalf("resumable with h %t, n %t", c.h, c.n)
		}
		l.cut()
		for range 2 {
			select {
			case <-l.served:
			case <-time.After(5 * time.Second):
				t.Fatalf("h %t, n %t: a side still serves after the cut", c.h, c.n)
			}
		}
		select {
		case err := <-l.hLost:
			t.Fatalf("h told lost (%v) on a connection that cannot resume", err)
		default:
		}
	}
}

// A token is the connection's, once: one from before the last resume, a made-up one, or one given to another
// process (a peer with nothing to resume) is refused, and nothing is written on the new transport.
func TestResumeTokenOnceAndOwn(t *testing.T) {
	l := newResumeLink(t, netshape.Config{}, 5*time.Second, true, true, nil)
	l.h.mu.Lock()
	first := l.h.resume.token
	l.h.mu.Unlock()
	l.cut()
	l.waitLost()
	if err := l.resume(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.h.Call(ctx, "echo", nil, nil); err != nil { // the new transport carried something: the answer came
		t.Fatal(err)
	}
	other := New("n", io.Discard)
	other.EnableLanes(nil, nil)
	other.EnableResume(time.Second)
	for name, c := range map[string]struct {
		p     *Peer
		token string
	}{
		"used":    {l.n, first},
		"made up": {l.n, newResumeToken()},
		"other":   {other, first},
	} {
		var w bytes.Buffer
		if err := c.p.AcceptResume(ResumeRequest{ID: "h99", Token: c.token}, strings.NewReader(""), &w); !errors.Is(err, ErrResumeRefused) {
			t.Errorf("%s token: %v", name, err)
		}
		if w.Len() > 0 {
			t.Errorf("%s token: wrote %q", name, w.String())
		}
	}
	if l.n.Lost() {
		t.Fatal("a refused token suspended the connection")
	}
	var out map[string]any
	if err := l.h.Call(ctx, "echo", nil, &out); err != nil {
		t.Fatalf("after the refusals: %v", err)
	}
}

// The node side may not have seen its transport fail when the manager comes back on a new one (a half-open link):
// the resume takes the connection over all the same, and the old transport is let go.
func TestResumeBeforeTheOtherSideNoticed(t *testing.T) {
	l := newResumeLink(t, netshape.Config{}, 5*time.Second, true, true, nil)
	l.mu.Lock()
	oldA, oldB := l.a, l.b
	l.mu.Unlock()
	oldA.Close() // the manager's end only: the node's still looks open
	select {
	case <-l.hLost:
	case <-time.After(5 * time.Second):
		t.Fatal("h not told lost")
	}
	if err := l.resume(); err != nil {
		t.Fatal(err)
	}
	oldB.Close() // as the resident node does once a resume took over
	var out map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.h.Call(ctx, "echo", nil, &out); err != nil {
		t.Fatalf("after resuming: %v", err)
	}
	if l.n.Lost() || l.h.Lost() {
		t.Fatal("still suspended after resuming")
	}
}

// What a writer keeps for a resume is bounded: past resumeKeepMax nothing is kept through the frame that went over (a
// gap), and the connection is resumable again once the other side acknowledges past it.
func TestResumeKeepIsBounded(t *testing.T) {
	old := resumeKeepMax
	resumeKeepMax = 100 << 10
	t.Cleanup(func() { resumeKeepMax = old })
	lw := newLaneWriter(io.Discard, make(chan struct{}), func(error) {}, nil)
	lw.keep = true
	now := time.Now()
	frame := make([]byte, 30<<10)
	for seq := uint64(1); seq <= 3; seq++ {
		lw.seq = seq
		lw.keepLocked(seq, frame, now)
	}
	if !lw.resumable() || !lw.canResume(0) {
		t.Fatal("90 KiB kept: not resumable")
	}
	lw.seq = 4
	lw.keepLocked(4, frame, now) // 120 KiB: over
	if lw.keptBytes != 0 || lw.resumable() || lw.canResume(2) {
		t.Fatalf("over the bound: %d bytes kept, resumable %t", lw.keptBytes, lw.resumable())
	}
	lw.seq = 5
	lw.keepLocked(5, frame, now)
	if err := lw.acked(4, 0); err != nil {
		t.Fatal(err)
	}
	if !lw.resumable() || !lw.canResume(4) || lw.canResume(3) {
		t.Fatal("the gap acknowledged: not resumable from it")
	}
	lw.seq = 6
	lw.keepLocked(6, frame, now.Add(resumeKeepAge+time.Second)) // the oldest kept too old: the other side is not reading
	if lw.resumable() {
		t.Fatal("frames kept unacknowledged past resumeKeepAge: still resumable")
	}
}

// pendingMethods is what a test waits on: the calls waiting for an answer.
func (p *Peer) pendingMethods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, m := range p.methods {
		out = append(out, m)
	}
	return out
}

// After a resume a frame may come again that was read over the old transport after this side told how far it read
// (the other side took the connection back before this one saw the old transport fail): it is dropped, and the line
// it belongs to comes once.
func TestResumeDropsFramesReadTwice(t *testing.T) {
	line := func(i int) []byte { return []byte(fmt.Sprintf(`{"event":"e","params":{"i":%d}}`, i)) }
	var lines [][]byte
	for i := 1; i <= 7; i++ {
		lines = append(lines, line(i))
	}
	first, _ := frames(0, LaneEvents, framePayloadMax, lines[:5]...) // 1..5 over the old transport
	again, _ := frames(2, LaneEvents, framePayloadMax, lines[2:]...) // 3..7 written again after a resume
	fr := newFrameReader(&resumedReader{parts: [][]byte{first, again}}, 64<<20)
	var got []string
	for fr.Scan() {
		got = append(got, string(fr.Bytes()))
	}
	if err := fr.Err(); err != nil {
		t.Fatalf("after %d lines: %v", len(got), err)
	}
	if len(got) != 7 {
		t.Fatalf("%d lines, want 7: %q", len(got), got)
	}
	for i, g := range got {
		if g != string(lines[i]) {
			t.Fatalf("line %d is %s", i+1, g)
		}
	}
}

// resumedReader hands out its parts one after the other, errResumed between two as a replaced transport does.
type resumedReader struct {
	parts [][]byte
	gap   bool
}

func (r *resumedReader) Read(b []byte) (int, error) {
	if len(r.parts) == 0 {
		return 0, io.EOF
	}
	if r.gap {
		r.gap = false
		return 0, errResumed
	}
	n := copy(b, r.parts[0])
	if r.parts[0] = r.parts[0][n:]; len(r.parts[0]) == 0 {
		r.parts = r.parts[1:]
		r.gap = len(r.parts) > 0
	}
	return n, nil
}

// A resume writes again only what the other side did not read: the frames after the last one it read.
func TestResumeWritesAgainFromWhatWasRead(t *testing.T) {
	lw := newLaneWriter(io.Discard, make(chan struct{}), func(error) {}, nil)
	lw.keep = true
	now := time.Now()
	for seq := uint64(1); seq <= 5; seq++ {
		lw.seq = seq
		f, _ := frames(seq-1, LaneEvents, framePayloadMax, []byte(fmt.Sprintf(`{"event":"e%d"}`, seq)))
		lw.keepLocked(seq, f, now)
	}
	lw.resume(io.Discard, 1, 3)
	lw.mu.Lock()
	batch := lw.resendLocked(nil)
	lw.mu.Unlock()
	heads, _ := heads(t, batch)
	var seqs []uint64
	for _, h := range heads {
		if h.kind == frameData {
			seqs = append(seqs, h.seq)
		}
	}
	if fmt.Sprint(seqs) != "[4 5]" {
		t.Fatalf("written again: %v, want [4 5]", seqs)
	}
}

// A read loop held up past the silence check (a slow handler on the manager's side, #10829's review): the other side
// takes the transport for dead and closes it, so the held side learns as soon as it reads again, and resumes at once,
// well within the window, rather than after its pings went unanswered. Nothing is lost or doubled.
func TestResumeHeldReaderLearnsAtOnce(t *testing.T) {
	oldStale, oldCheck := resumeStale, resumeCheck
	resumeStale, resumeCheck = 200*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { resumeStale, resumeCheck = oldStale, oldCheck })
	hold := make(chan struct{})
	var mu sync.Mutex
	var got []int
	l := newResumeLink(t, netshape.Config{}, 5*time.Second, true, true, func(h, n *Peer) {
		h.OnEvent = func(_ string, params json.RawMessage) {
			var e struct{ I int }
			_ = json.Unmarshal(params, &e)
			if e.I == 0 {
				<-hold
			}
			mu.Lock()
			got = append(got, e.I)
			mu.Unlock()
		}
	})
	const events = 200
	go func() {
		for i := range events {
			_ = l.n.Notify("ev", map[string]int{"i": i})
		}
	}()
	select {
	case <-l.nLost: // its frames unacknowledged, nothing read
	case <-time.After(5 * time.Second):
		t.Fatal("the node's side never took the transport for dead")
	}
	close(hold)
	select {
	case <-l.hLost:
	case <-time.After(2 * time.Second):
		t.Fatal("the held side was not told: it would have waited for its pings, past the other side's window")
	}
	if err := l.resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == events
	})
	mu.Lock()
	defer mu.Unlock()
	for i, v := range got {
		if v != i {
			t.Fatalf("event %d came as %d: %v", i, v, got)
		}
	}
}
