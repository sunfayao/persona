package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Pausing the upstream while the bound conversation works (docs/assistant/voice.md 4.9 "Suspension", suspend.go).

// sessionsUp is a live service that keeps each session's frames apart, in the order the sessions began; with hand, its
// model hands every line the user types over as a client delegation (dlg1, dlg2…). refuse says whether session n
// (from 0) is refused: closed before it starts; drops (set before the call) whether it is closed as soon as it started;
// slow (set before the call, slowStart) what session n's start waits for: it starts once that is closed (nil: at once).
// It starts a realtime dialect's session too (its session.update), as an otherwise silent service.
type sessionsUp struct {
	*fakeUpstream
	mu       sync.Mutex
	sessions [][]map[string]any
	conns    []*websocket.Conn
	ids      map[*websocket.Conn]int
	drops    func(n int) bool
	slow     func(n int) <-chan struct{}
}

func newSessionsUp(t *testing.T, hand bool, refuse func(n int) bool) *sessionsUp {
	s := &sessionsUp{ids: map[*websocket.Conn]int{}}
	var dlg atomic.Int32
	s.fakeUpstream = newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		s.mu.Lock()
		n, ok := s.ids[c]
		if !ok {
			n = len(s.sessions)
			s.ids[c] = n
			s.sessions, s.conns = append(s.sessions, nil), append(s.conns, c)
		}
		s.sessions[n] = append(s.sessions[n], f)
		s.mu.Unlock()
		switch f["type"] {
		case "session.start", "session.update":
			if refuse != nil && refuse(n) {
				_ = c.Close(websocket.StatusGoingAway, "not now")
				return
			}
			if s.slow != nil {
				if wait := s.slow(n); wait != nil {
					<-wait
				}
			}
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
			if s.drops != nil && s.drops(n) {
				_ = c.Close(websocket.StatusGoingAway, "upstream restarts")
			}
		case "session.commentary.append":
			if hand && f["delegation_id"] == nil && f["event_id"] == nil {
				delegate(u, c, fmt.Sprintf("dlg%d", dlg.Add(1)), content(f))
			}
		}
	})
	return s
}

// slowStart makes session n slow to start (set before the call): its start waits until release, which the test calls
// once it has done what it does meanwhile, and which the test's end calls at the latest (a failed assertion leaves no
// start waiting).
func (s *sessionsUp) slowStart(t *testing.T, n int) (release func()) {
	started := make(chan struct{})
	release = sync.OnceFunc(func() { close(started) })
	t.Cleanup(release)
	s.slow = func(i int) <-chan struct{} {
		if i == n {
			return started
		}
		return nil
	}
	return release
}

// count is how many sessions began.
func (s *sessionsUp) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// frames is what session n got so far.
func (s *sessionsUp) frames(n int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n >= len(s.sessions) {
		return nil
	}
	return slices.Clone(s.sessions[n])
}

// find waits for a frame of session n that matches.
func (s *sessionsUp) find(t *testing.T, n int, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	var got map[string]any
	waitFor(t, fmt.Sprintf("session %d: %s", n, what), func() bool {
		i := slices.IndexFunc(s.frames(n), match)
		if i >= 0 {
			got = s.frames(n)[i]
		}
		return i >= 0
	})
	return got
}

// audio is the amplitude of the first sample of each frame of the app's audio session n got, in order.
func (s *sessionsUp) audio(n int) (amps []int16, bytes []int) {
	for _, f := range s.frames(n) {
		if f["type"] != "session.input_audio.append" {
			continue
		}
		b, _ := base64.StdEncoding.DecodeString(f["audio"].(string))
		if len(b) < 2 {
			continue
		}
		amps, bytes = append(amps, int16(uint16(b[0])|uint16(b[1])<<8)), append(bytes, len(b))
	}
	return amps, bytes
}

// conn is session n's socket, for the test to send its events on.
func (s *sessionsUp) conn(n int) *websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns[n]
}

// drop closes session n's socket, as a service that restarts does.
func (s *sessionsUp) drop(n int) {
	s.mu.Lock()
	c := s.conns[n]
	s.mu.Unlock()
	_ = c.Close(websocket.StatusGoingAway, "upstream restarts")
}

func linkIn(state string) func(map[string]any) bool {
	return func(f map[string]any) bool { return f["type"] == "link" && f["state"] == state }
}

// workingAgent is agentHub with a1 working on a turn begun at the computer (the call follows it as an observed run),
// left to the test to answer.
func workingAgent(t *testing.T) *fakeHub {
	hub := agentHub(t)
	hub.manual = true
	hub.add("a1", proto.Item{Role: "user", Kind: proto.KindText, Text: "跑一下集成测试"})
	hub.status("a1", "working")
	return hub
}

// quick makes the call look and pause in a test's time.
func quick(t *testing.T) {
	setVar(t, &lookEvery, 100*time.Millisecond)
	setVar(t, &pauseAfter, 500*time.Millisecond)
}

// untilAll reads the app's frames until each of match has matched one, in any order.
func (a *app) untilAll(match ...func(map[string]any) bool) {
	a.t.Helper()
	left := slices.Clone(match)
	a.until(func(f map[string]any) bool {
		left = slices.DeleteFunc(left, func(m func(map[string]any) bool) bool { return m(f) })
		return len(left) == 0
	})
}

// recLink is an upstream session that keeps the frames written to it; once broken it refuses the next write, after
// broke (as the session's reader noticing the drop).
type recLink struct {
	mu     sync.Mutex
	frames []map[string]any
	left   int // writes it takes before it breaks; < 0: it never does
	broke  func()
}

func (l *recLink) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

func (l *recLink) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	l.mu.Lock()
	if l.left == 0 {
		l.mu.Unlock()
		l.broke()
		return net.ErrClosed
	}
	l.left--
	var f map[string]any
	_ = json.Unmarshal(p, &f)
	l.frames = append(l.frames, f)
	l.mu.Unlock()
	return nil
}

func (l *recLink) Close(websocket.StatusCode, string) error { return nil }
func (l *recLink) CloseNow() error                          { return nil }

// sent is what it got: an audio frame as its first sample, a typed line as itself.
func (l *recLink) sent() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, f := range l.frames {
		if f["type"] == "session.input_audio.append" {
			b, _ := base64.StdEncoding.DecodeString(f["audio"].(string))
			out = append(out, fmt.Sprint(b[0]))
		} else {
			out = append(out, content(f))
		}
	}
	return out
}

// A woken session that drops before what was kept for it has gone out leaves the rest kept, in order: the session lost
// reconnects to gets it (lost flushes once it is up), the lines typed still counting as the user speaking then.
func TestFlushAfterTheWokenSessionDropped(t *testing.T) {
	c := &conversation{version: 2, pl: &Plugin{logger: log.New(io.Discard, "", 0)}, ctx: t.Context(), w: liveWire{},
		wake: make(chan struct{}, 1)}
	c.sus.state = linkWaking
	for i := range 3 {
		c.sus.ring.add([]byte{byte(i + 1), 0}, 1<<20)
	}
	c.sus.texts = []string{"数字是几", "顺便看看日志"}
	want := []string{"1", "2", "3", "数字是几", "顺便看看日志"}

	c.flushRing() // it dropped before this began: lost took it off
	if c.sus.state != linkWaking || c.sus.flush || len(c.sus.ring.frames) != 3 || len(c.sus.texts) != 2 {
		t.Fatalf("not kept: %+v", c.sus)
	}

	// The next one drops after two frames: its reader sees it and lost takes it off.
	first := &recLink{left: 2}
	first.broke = func() {
		c.mu.Lock()
		if c.up == first {
			c.gen++
			c.up, c.ready = nil, false
		}
		c.mu.Unlock()
	}
	c.up, c.ready = first, true
	c.flushRing()
	if got := first.sent(); !slices.Equal(got, want[:2]) {
		t.Fatalf("the dropped session got %v", got)
	}
	if c.sus.state != linkWaking || c.sus.flush || len(c.sus.ring.frames) != 1 || len(c.sus.texts) != 2 || len(c.history) != 0 {
		t.Fatalf("not kept: %+v, history %v", c.sus, c.history)
	}

	// Its line breaks it too: the line is kept, not taken as said.
	second := &recLink{left: 1}
	second.broke = func() {
		c.mu.Lock()
		if c.up == second {
			c.gen++
			c.up, c.ready = nil, false
		}
		c.mu.Unlock()
	}
	c.up, c.ready = second, true
	c.flushRing()
	if got := second.sent(); !slices.Equal(got, want[2:3]) || len(c.sus.texts) != 2 || len(c.history) != 0 {
		t.Fatalf("the second session got %v; kept %v, history %v", got, c.sus.texts, c.history)
	}

	c.mu.Lock()
	c.gate.told(time.Now(), agentReconnect) // the reconnected session's snapshot has the agent's new words
	at := c.gate.agent
	c.mu.Unlock()
	third := &recLink{left: -1}
	c.up, c.ready = third, true
	c.flushRing()
	if got := third.sent(); !slices.Equal(got, want[3:]) {
		t.Fatalf("the reconnected session got %v", got)
	}
	if c.sus.state != "" || c.sus.flush || len(c.history) != 2 || c.history[1].Text != "顺便看看日志" {
		t.Fatalf("after: %+v, history %v", c.sus, c.history)
	}
	if !c.gate.openSince(at, time.Now()) {
		t.Fatal("the lines typed do not count as the user speaking")
	}
	select {
	case <-c.wake:
	default:
		t.Fatal("what waits to be said was not let go")
	}
}

// A card told to this upstream session is not asked again to it, however late the tracker comes to the new session
// (reask); one told to the session before is.
func TestToldBefore(t *testing.T) {
	c := &conversation{}
	c.startAsk("q1")
	if c.toldBefore("q1") {
		t.Fatal("not told yet")
	}
	c.gen++ // a wake: the question was queued, and is told to the new session
	c.told("q1")
	if c.toldBefore("q1") || c.toldBefore("q2") {
		t.Fatal("told to this session: asked again")
	}
	c.gen++ // it dropped
	if !c.toldBefore("q1") {
		t.Fatal("told to the last session: not asked again")
	}
	c.told("q1") // asked again
	if c.toldBefore("q1") {
		t.Fatal("asked again to this session: asked once more")
	}
}

// What holds a pause off: each of these, set alone on a call that may pause, does.
func TestPauseDue(t *testing.T) {
	now := time.Now()
	recent, long := now.Add(-pauseAfter/2), now.Add(-2*pauseAfter)
	may := func() *conversation {
		c := &conversation{version: 2, ready: true, up: newFakeLink(), pending: map[string]*time.Timer{}}
		c.sus.works, c.connectedAt = true, long
		return c
	}
	if !may().pauseDue(now) {
		t.Fatal("a quiet call whose conversation works does not pause")
	}
	// What still holds a pause off on hold (#9290): no session to pause. Everything else does not.
	hard := map[string]bool{"protocol 1": true, "paused": true, "reconnecting": true, "over": true}
	for what, set := range map[string]func(c *conversation){
		"protocol 1":             func(c *conversation) { c.version = 1 },
		"it does not work":       func(c *conversation) { c.sus.works = false },
		"paused":                 func(c *conversation) { c.sus.state = linkPaused },
		"reconnecting":           func(c *conversation) { c.ready, c.up = false, nil },
		"over":                   func(c *conversation) { c.closed = true },
		"the model speaks":       func(c *conversation) { c.pace.playEnd = now.Add(time.Second) },
		"the model spoke lately": func(c *conversation) { c.pace.playEnd = recent },
		"the user's voice":       func(c *conversation) { c.pace.voice = recent },
		"the user's caption":     func(c *conversation) { c.gate.user = recent },
		"the user's open line":   func(c *conversation) { c.user = &line{id: "u1", text: "等一下"} },
		"a handover":             func(c *conversation) { c.pace.delegated = recent },
		"told the model":         func(c *conversation) { c.sus.said = recent },
		"a new session":          func(c *conversation) { c.connectedAt = recent },
		"something to say":       func(c *conversation) { c.queue = []*sentWords{{}} },
		"a call to come":         func(c *conversation) { c.pending["d1"] = nil },
		"a card asked":           func(c *conversation) { c.askCur = "q1" },
	} {
		c := may()
		set(c)
		if c.pauseDue(now) {
			t.Errorf("%s: it pauses", what)
		}
		c = may()
		set(c)
		c.sus.onHold = true
		if c.pauseDue(now) != !hard[what] {
			t.Errorf("%s, on hold: it pauses %v", what, c.pauseDue(now))
		}
	}
	idle := may()
	idle.sus.works, idle.connectedAt, idle.sus.onHold = false, now, true
	if !idle.pauseDue(now) {
		t.Error("on hold: an idle call that just connected does not pause")
	}
}

// The ring keeps the last frames up to its size, oldest first, and empties as it is taken.
func TestRing(t *testing.T) {
	var r ring
	for i := range 10 {
		r.add([]byte{byte(i), 0}, 7)
	}
	r.add(nil, 7)
	got := r.take()
	if len(got) != 3 || got[0][0] != 7 || got[2][0] != 9 {
		t.Fatalf("kept %v", got)
	}
	if len(r.take()) != 0 || r.size != 0 {
		t.Fatal("not emptied")
	}
	r.add(make([]byte, 20), 7) // a frame larger than it all is kept alone
	if got := r.take(); len(got) != 1 {
		t.Fatalf("kept %d frames", len(got))
	}
}

// A long call's history with a full snapshot stays within instructionsMost: its oldest lines go first.
func TestInstructionsFit(t *testing.T) {
	var history []prompts.VoiceLine
	for i := range historyLines {
		history = append(history, prompts.VoiceLine{Role: "user", Text: fmt.Sprintf("line %d %s", i, strings.Repeat("word ", 40))})
	}
	snap := &prompts.VoiceTarget{Kind: "main", State: "working", Title: strings.Repeat("t", 120), Goal: strings.Repeat("g", 200),
		Work: strings.Repeat("w", 200)}
	for range recentMost {
		snap.Recent = append(snap.Recent, prompts.VoiceLine{Role: "assistant", Text: strings.Repeat("r", recentRunes)})
	}
	for range agentsMost {
		snap.Agents = append(snap.Agents, prompts.VoiceAgent{Title: strings.Repeat("a", 80), State: "working"})
	}
	in := prompts.VoiceInstructions{Lang: "en", Tools: "bound", History: clipHistory(history), Target: snap, Paused: true}
	if utf8.RuneCountInString(prompts.RenderVoiceInstructions(in)) <= instructionsMost {
		t.Fatal("the test's instructions fit as they are")
	}
	got := fitInstructions(in)
	if n := utf8.RuneCountInString(got); n > instructionsMost || !strings.Contains(got, fmt.Sprintf("line %d ", historyLines-1)) ||
		strings.Contains(got, "line 0 ") {
		t.Fatalf("%d runes: %s", n, got)
	}
}
