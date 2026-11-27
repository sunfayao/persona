package voice

import (
	"context"
	"testing"
	"time"
)

// Nudging a model that owes the user an answer and says nothing (#2297, nudge.go, docs/assistant/voice.md 4.5 rule 9).

// When a nudge is due, and every case that holds it back (nudge.go).
func TestNudgeDue(t *testing.T) {
	now := time.Now()
	owedAt := now.Add(-nudgeAfter - time.Second)
	base := func() *conversation {
		c := &conversation{ready: true, up: newFakeLink()}
		c.connectedAt = owedAt.Add(-time.Minute)
		c.gate.user = owedAt
		return c
	}
	for _, tc := range []struct {
		name   string
		result bool
		set    func(c *conversation)
		due    bool
	}{
		{"owed and quiet", false, nil, true},
		{"a result owed", true, nil, true},
		{"nothing owed", false, func(c *conversation) { c.owed = owedAnswer{} }, false},
		{"nudged already", false, func(c *conversation) { c.owed.nudged = true }, false},
		{"not owed long enough", false, func(c *conversation) { c.owed.at = now.Add(-nudgeAfter + time.Second) }, false},
		{"the model's speech played out lately", false, func(c *conversation) { c.pace.playEnd = now.Add(-time.Second) }, false},
		{"its session came up lately", false, func(c *conversation) { c.connectedAt = now.Add(-time.Second) }, false},
		{"the user's voice lately", false, func(c *conversation) { c.pace.voice = now.Add(-nudgeQuiet / 2) }, false},
		{"the user's caption grew lately", false, func(c *conversation) { c.userAt = now.Add(-nudgeQuiet / 2) }, false},
		{"the user typed lately", false, func(c *conversation) { c.gate.user = now.Add(-nudgeQuiet / 2) }, false},
		{"closed", false, func(c *conversation) { c.closed = true }, false},
		{"reconnecting", false, func(c *conversation) { c.ready, c.up = false, nil }, false},
		{"paused", false, func(c *conversation) { c.ready, c.up, c.sus.state = false, nil, linkPaused }, false},
		{"waking", false, func(c *conversation) { c.sus.state = linkWaking }, false},
		{"no echo cancellation", false, func(c *conversation) { c.gate.echoes = true }, false},
		{"a card asked", false, func(c *conversation) { c.askCur = "q1" }, false},
		{"something to say", false, func(c *conversation) { c.queue = []*sentWords{{}} }, false},
		{"the bound conversation works", false, func(c *conversation) { c.sus.works = true }, false},
		{"a run works", false, func(c *conversation) { c.open = []openTask{{id: "r1", task: "x", state: runWorking}} }, false},
		{"a run is held", false, func(c *conversation) { c.open = []openTask{{id: "r1", task: "x", state: runHeld}} }, false},
		{"a run waits on the user", false, func(c *conversation) { c.open = []openTask{{id: "r1", task: "x", state: runNeedsYou}} }, true},
		{"a result while a run works", true, func(c *conversation) {
			c.sus.works, c.open = true, []openTask{{id: "r1", task: "x", state: runWorking}}
		}, true},
	} {
		c := base()
		c.owed = owedAnswer{at: owedAt, cause: owedTyped, result: tc.result}
		if tc.set != nil {
			tc.set(c)
		}
		if got := c.nudgeDue(now); got != tc.due {
			t.Errorf("%s: due %v, want %v", tc.name, got, tc.due)
		}
	}
}

// What makes an answer owed, and what answers it (nudge.go).
func TestAnswerOwed(t *testing.T) {
	now := time.Now()
	spoke := now.Add(-2 * time.Second) // the user stopped saying their line
	for _, tc := range []struct {
		name string
		set  func(c *conversation)
		owed bool
	}{
		{"nothing since", nil, true},
		{"the model spoke before they stopped", func(c *conversation) { c.modelAt = spoke.Add(-time.Second) }, true},
		{"the model answered after they stopped", func(c *conversation) { c.modelAt = spoke.Add(time.Second) }, false},
		{"the model spoke on over them", func(c *conversation) { c.pace.playEnd = spoke.Add(time.Second) }, false},
	} {
		c := &conversation{}
		if tc.set != nil {
			tc.set(c)
		}
		c.userLineDone(spoke)
		if owed := !c.owed.at.IsZero(); owed != tc.owed || owed && (c.owed.cause != owedCaption || c.owed.result) {
			t.Errorf("a caption, %s: %+v", tc.name, c.owed)
		}
	}

	c := &conversation{ready: true, up: newFakeLink()}
	for _, tc := range []struct {
		name  string
		s     *sentWords
		cause string // "" none
	}{
		{"a result", &sentWords{delegation: "dlg1"}, owedResult},
		{"a say", &sentWords{}, owedResult},
		{"a tool's result", &sentWords{delegation: "dlg1", onCall: true}, owedToolResult},
		{"a heartbeat", &sentWords{prio: prioHeartbeat}, ""},
		{"the nudge", &sentWords{nudge: true}, ""},
	} {
		c.owed = owedAnswer{}
		c.handedOver(tc.s, now)
		if c.owed.cause != tc.cause || tc.cause != "" && !c.owed.result {
			t.Errorf("handed over %s: %+v", tc.name, c.owed)
		}
	}
	c.ready, c.up = false, nil
	c.owed = owedAnswer{}
	if c.handedOver(&sentWords{}, now); !c.owed.at.IsZero() {
		t.Errorf("owed by a session that is not up: %+v", c.owed)
	}

	// The rest of an answer the app cut off (interrupt) is dropped: the user never hears it, so it answers nothing.
	// Audio that goes to the app does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cut := &conversation{ctx: ctx, out: make(chan outFrame, 1), dropAudio: true, lastAudio: time.Now()}
	cut.owe(time.Now(), owedTyped, false)
	if cut.handle(0, event{kind: evAudio, audio: make([]byte, 320)}); cut.owed.at.IsZero() || !cut.modelAt.IsZero() {
		t.Errorf("answered by audio that was dropped: %+v", cut.owed)
	}
	cut.dropAudio = false
	if cut.handle(0, event{kind: evAudio, audio: make([]byte, 320)}); !cut.owed.at.IsZero() || cut.modelAt.IsZero() {
		t.Errorf("not answered by audio the app got: %+v", cut.owed)
	}

	c.owe(now, owedTyped, false)
	c.owed.nudged = true
	c.owe(now, owedCaption, false)
	if c.owed.nudged {
		t.Error("a new answer owed is not armed again")
	}
	c.modelActed(now)
	if !c.owed.at.IsZero() || !c.modelAt.Equal(now) {
		t.Errorf("answered: %+v", c.owed)
	}
}
