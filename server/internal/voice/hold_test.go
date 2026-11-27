package voice

import (
	"io"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Holding a call (#9290, docs/assistant/voice.md 4.9 "Suspension", suspend.go holdFrame): the app's hold frame pauses
// the upstream at once and nothing wakes it until the hold is off.

func hold(on bool) map[string]any { return map[string]any{"type": "hold", "on": on} }

// never reads the app's frames for d: none of them may match.
func (a *app) never(d time.Duration, what string, match func(map[string]any) bool) {
	a.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case f, ok := <-a.frames:
			if !ok {
				a.t.Fatal("the plugin closed the conversation")
			}
			if match(f) {
				a.t.Fatalf("%s: %v", what, f)
			}
		case <-deadline:
			return
		}
	}
}

// A hold pauses even while a card is asked by voice, which holds a quiet pause off: back from the hold, the new
// session asks it again, once.
func TestHoldWhileACardIsAsked(t *testing.T) {
	setVar(t, &lookEvery, 100*time.Millisecond)
	hub := workingAgent(t)
	hub.raise(proto.Interaction{ID: "q1", Kind: proto.IxQuestion, MachineID: "m", SessionID: "a1",
		Questions: []proto.Question{{Question: "数据库用哪个？", Options: []proto.QuestionOption{{Label: "Postgres"}, {Label: "SQLite"}}}}})
	up := newSessionsUp(t, false, nil)
	pl := testPlugin(t, liveProvider(up.fakeUpstream), hub.server(t))
	logs := captureLog(t, pl)
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.until(func(f map[string]any) bool { return f["type"] == "ask" && f["state"] == askAsking })
	a.send(hold(true))
	a.until(linkIn(linkPaused))
	a.send(hold(false))
	a.until(linkIn(linkLive))
	waitFor(t, "the question in the new session", func() bool { return cardAsks(up, 1) > 0 })
	askedOnce(t, a, hub, up, logs, 1)
}

// A hold that comes as the upstream wakes (the user took the call off hold and put it on hold again at once) pauses
// the woken session once it is up, long before pauseAfter; its off wakes it again.
func TestHoldWhileWaking(t *testing.T) {
	setVar(t, &pauseAfter, time.Hour)
	hub := agentHub(t)
	up := newSessionsUp(t, false, nil)
	pl := testPlugin(t, liveProvider(up.fakeUpstream), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.send(hold(true))
	a.until(linkIn(linkPaused))
	a.send(hold(false))
	a.send(hold(true))
	a.until(linkIn(linkWaking))
	a.until(linkIn(linkLive))
	a.until(linkIn(linkPaused))
	if n := up.count(); n != 2 {
		t.Fatalf("%d sessions", n)
	}
	up.find(t, 1, "session.close", ofType("session.close"))
	a.send(hold(false))
	a.until(linkIn(linkLive))
	if n := up.count(); n != 3 {
		t.Fatalf("%d sessions", n)
	}
}

// The call goes on hold again as it wakes (off, and on again before the woken session is up), and the user types a
// line during that hold: the woken session is paused before anything kept goes to it, and the line goes to the one
// the next off wakes, once.
func TestHoldAgainAsItWakes(t *testing.T) {
	setVar(t, &pauseAfter, time.Hour)
	hub := agentHub(t)
	up := newSessionsUp(t, false, nil)
	release := up.slowStart(t, 1)
	pl := testPlugin(t, liveProvider(up.fakeUpstream), hub.server(t))
	logs := captureLog(t, pl)
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.send(hold(true))
	a.until(linkIn(linkPaused))
	a.send(hold(false))
	a.until(linkIn(linkWaking))
	a.send(hold(true))
	a.send(map[string]any{"type": "text", "text": "数字是几"})
	a.until(finalLine("user", "数字是几")) // read after the hold: both came before the woken session is up
	release()
	a.until(linkIn(linkLive))
	a.until(linkIn(linkPaused))
	up.find(t, 1, "session.close", ofType("session.close"))
	logs.wait("call 1: paused the upstream (2): session 2 closed, the call is on hold")
	if n := typedIn(up, 1, "数字是几"); n != 0 {
		t.Fatal("the line typed on hold went to the session woken as the hold came")
	}
	a.send(hold(false))
	a.until(linkIn(linkLive))
	waitFor(t, "the typed line", func() bool { return typedIn(up, 2, "数字是几") > 0 })
	wokeThrough(t, a, up, logs, 2)
	if n := typedIn(up, 2, "数字是几"); n != 1 || up.count() != 3 {
		t.Fatalf("sent %d times, %d sessions", n, up.count())
	}
}

// What nextToSay took goes back to the queue when the session goes before it is sent (the speaker is between the two
// as readApp takes a hold, or a write meets a pause): the next session says it. Nothing of it goes to the old one.
func TestHoldBetweenTakingAndSaying(t *testing.T) {
	result := func() *sentWords { return &sentWords{words: plainWords("集成测试全部通过。"), agent: true} }
	call := func(up *recLink) *conversation {
		c := &conversation{version: 2, pl: &Plugin{logger: log.New(io.Discard, "", 0)}, ctx: t.Context(), w: liveWire{},
			out: make(chan outFrame, 64), wake: make(chan struct{}, 1), unacked: map[string]*sentWords{}}
		c.up, c.ready = up, true
		return c
	}
	kept := func(c *conversation, s *sentWords) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.queue) == 1 && c.queue[0] == s
	}
	saidTo := func(up *recLink) bool { return slices.Contains(up.sent(), "集成测试全部通过。") }

	for _, paused := range []bool{true, false} { // the hold paused the session, or it is on hold and not paused yet
		up := &recLink{left: -1}
		c := call(up)
		c.queue = []*sentWords{result()}
		s := c.nextToSay(time.Now())
		if s == nil {
			t.Fatal("nothing to say")
		}
		if paused {
			c.holdFrame(true)
		} else {
			c.mu.Lock()
			c.sus.onHold = true // as a reconnected session is, before the speaker's next look pauses it
			c.mu.Unlock()
		}
		c.sendNow(s)
		if saidTo(up) || !kept(c, s) {
			t.Fatalf("paused %v: said %v, queue %v", paused, up.sent(), c.queue)
		}
	}

	// The session goes as it is written to (a hold's pause closes it): the write fails, and it is kept.
	up := &recLink{left: 0}
	c := call(up)
	up.broke = func() {
		c.mu.Lock()
		c.gen++
		c.up, c.ready = nil, false
		c.mu.Unlock()
	}
	s := result()
	c.sendNow(s)
	if !kept(c, s) {
		t.Fatalf("a write the pause failed: queue %v", c.queue)
	}

	// A write that fails on a session still up is not kept (its reader sees it and lost reconnects): the speaker would
	// only try it again on the same session.
	up = &recLink{left: 0, broke: func() {}}
	c = call(up)
	c.sendNow(result())
	if len(c.queue) != 0 {
		t.Fatalf("a failed write on the same session: queue %v", c.queue)
	}
}

// typedIn is how often session n got the typed line text.
func typedIn(up *sessionsUp, n int, text string) int {
	return len(slices.DeleteFunc(up.frames(n), func(f map[string]any) bool {
		return f["type"] != "session.commentary.append" || f["delegation_id"] != nil || content(f) != text
	}))
}

// cardAsks is how often session n got the card's question (数据库用哪个).
func cardAsks(up *sessionsUp, n int) int {
	return len(slices.DeleteFunc(up.frames(n), func(f map[string]any) bool { return !strings.Contains(content(f), "数据库用哪个") }))
}

// followed waits until the call follows the turn a1 works on (workingAgent) as an observed run: what that turn brings
// is the call's to say.
func (l *logCapture) followed() {
	l.t.Helper()
	l.wait("call 1: run observed-1: the bound conversation works on a turn the call did not hand over: followed")
}

// trackerDone waits until the call's tracker has done every op it was given before now (a new session's reask): the
// app's resend of a run the call does not know (id) is one more op in its queue, done in turn, and logged.
func (a *app) trackerDone(logs *logCapture, id string) {
	a.t.Helper()
	a.send(map[string]any{"type": "resend", "id": id})
	logs.wait("call 1: resend " + id + ": no such refused run")
}

// wokeThrough waits until what the call kept for its woken session n has all come in: the wake is over (logged once
// all of it went out), and a line typed then reaches n after it.
func wokeThrough(t *testing.T, a *app, up *sessionsUp, logs *logCapture, n int) {
	t.Helper()
	logs.wait("call 1: woke the upstream in")
	a.send(map[string]any{"type": "text", "text": "还在吗"})
	waitFor(t, "the line typed once it woke", func() bool { return typedIn(up, n, "还在吗") > 0 })
}

// askedOnce checks that the woken session n asked the card once, however late the tracker came to it. The wake is
// over (logged after its connect gave the tracker the new session's reask), the tracker did all it was given by then
// (trackerDone), and then the agent's result, which goes after any copy of the question queued before it (a card goes
// first, and nothing queued later is let go sooner), reached n.
func askedOnce(t *testing.T, a *app, hub *fakeHub, up *sessionsUp, logs *logCapture, n int) {
	t.Helper()
	logs.wait("call 1: woke the upstream in")
	a.trackerDone(logs, "r-after-wake")
	logs.followed()
	hub.add("a1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "集成测试全部通过。"})
	hub.end("a1")
	up.find(t, n, "the result, said after the question", func(f map[string]any) bool {
		return said(f) && strings.Contains(content(f), "集成测试全部通过")
	})
	if got := cardAsks(up, n); got != 1 {
		t.Fatalf("asked %d times in session %d", got, n)
	}
}

// got: the app got a frame of type typ so far.
func (a *app) got(typ string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.types[typ]
}
