package voice

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// delegate makes the model hand text over as client delegation id.
func delegate(u *fakeUpstream, c *websocket.Conn, id, text string) {
	u.send(c, map[string]any{"type": "session.delegation.created", "delegation": map[string]any{"id": id, "target": "client",
		"content": []map[string]string{{"type": "input_text", "text": text}}}})
}

// docs/assistant/voice.md 4.5 rule 4: a service that refuses the result on its delegation (gone: expired, closed) has it said
// instead, so the turn's one answer is not lost.
func TestRefusedResultIsSaid(t *testing.T) {
	hub := newFakeHub(t)
	up := delegatingUpstreamWith(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		u.send(c, map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "code": "invalid_request",
			"message": "Expected an outstanding delegation id.", "param": "delegation_id", "client_event_id": f["event_id"]},
			"client_event_id": f["event_id"]})
	})
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "明天有什么会"})
	first := up.nextMatch(on("dlg1"))
	said := up.nextMatch(on(""))
	if content(said) != content(first) || !strings.Contains(content(said), "明天下午三点有会") || said["event_id"] == first["event_id"] {
		t.Fatalf("said %v after %v", said, first)
	}
}

// A service that holds a commentary to an append's 500 tokens refuses a long answer (param content): it goes again,
// within them, still a whole report (its block closed, its start and end).
func TestTooLongResultIsSentShorter(t *testing.T) {
	hub := newFakeHub(t)
	hub.reply = func(string) string {
		return "先说结论：都修好了。" + strings.Repeat("中间的过程很长。", 300) + "最后：测试全部通过。"
	}
	up := delegatingUpstreamWith(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		if tokens(content(f)) > 500 {
			u.send(c, map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "code": "invalid_request",
				"message": "Content exceeds the 500 token append limit.", "param": "content", "client_event_id": f["event_id"]}})
		}
	})
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "修一下测试"})
	if first := content(up.nextMatch(on("dlg1"))); tokens(first) <= 500 {
		t.Fatalf("the first went short already: %d tokens", int(tokens(first)))
	}
	got := content(up.nextMatch(on("dlg1")))
	if tokens(got) > appendTokens || !strings.Contains(got, "</agent_output>") || !strings.Contains(got, "先说结论") ||
		!strings.Contains(got, "测试全部通过") || !strings.Contains(got, "\"修一下测试\"") {
		t.Fatalf("again (%d tokens): %s", int(tokens(got)), got)
	}
}

// A report is whole within a commentary whatever it holds: a long answer is cut to the room the tasks leave it, not
// the report after it (its block would be left open).
func TestReportFitsACommentary(t *testing.T) {
	answer := "## 结论\n\n" + strings.Repeat("A long answer with `code` and many words in it. 很长的回答。", 200) + "\n\nThe end."
	for _, l := range i18n.Langs {
		for _, n := range []int{1, 6, 10} {
			var tasks []string
			for i := range n {
				tasks = append(tasks, clipRunes(fmt.Sprintf("第 %d 件：", i)+strings.Repeat("一件很长的事情", 20), taskMost))
			}
			rep := prompts.VoiceReport{Lang: string(l), Status: "done", Tasks: tasks, Background: 1}
			w := reportWords(l, rep, answer)
			if !strings.Contains(w.text, "</agent_output>") || !strings.Contains(w.text, "The end.") || !strings.Contains(w.short, "</agent_output>") {
				t.Errorf("%s, %d tasks: not whole: …%s", l, n, w.text[max(0, len(w.text)-300):])
			}
			if runeLen(w.text) > commentaryRunes {
				t.Errorf("%s, %d tasks: %d characters", l, n, runeLen(w.text))
			}
			if tokens(w.short) > appendTokens+1 {
				t.Errorf("%s, %d tasks: short is %d tokens", l, n, int(tokens(w.short)))
			}
			if !strings.Contains(w.text, tasks[n-1]) {
				t.Errorf("%s, %d tasks: the last task is missing", l, n)
			}
		}
	}
	// A short answer goes as it is.
	w := reportWords(i18n.ZhHans, prompts.VoiceReport{Lang: "zh-Hans", Status: "done", Tasks: []string{"查日程"}}, "明天下午三点有会")
	if !strings.Contains(w.text, "<agent_output>\n明天下午三点有会\n</agent_output>") || w.short != w.text {
		t.Fatalf("%q / %q", w.text, w.short)
	}
}

// A send that broke off without the hub's answer (the connection dropped: a redeploy) may have been typed: the model is
// told it is not known (look on the phone, do not repeat it), with nothing of the URL or the Go error in it.
func TestASendThatBreaksOffIsUnknown(t *testing.T) {
	hub := newFakeHub(t)
	hub.cut = true
	up := delegatingUpstream(t)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "明天有什么会"})
	got := content(up.nextMatch(on("dlg1")))
	want := prompts.RenderVoiceRefused(prompts.VoiceRefused{Lang: "zh-Hans", Code: "unknown", Task: "明天有什么会"})
	if got != want {
		t.Fatalf("told %s", got)
	}
}

// What the hub refuses with no words of its own (a bare 500) is told without the URL either.
func TestSendFailureWords(t *testing.T) {
	for _, c := range []struct {
		err          error
		code, reason string
	}{
		{&hubError{Status: 409, Message: "在等你先处理上面的卡片", Code: "dialog_open"}, "failed", "在等你先处理上面的卡片"},
		{&hubError{Status: 500, call: "POST /api/v1/sessions/m/s/messages"}, "failed", ""},
		{&hubError{Status: 502, Message: "x", Delivered: "unknown"}, "unknown", ""},
		{&hubError{Status: http.StatusGatewayTimeout, call: "POST /x"}, "unknown", ""},
		{maybeSent(&hubError{Status: 400, Message: "bad"}), "failed", "bad"},
		{maybeSent(errFake("Post \"http://127.0.0.1:1/api/v1/sessions/m/s/messages\": EOF")), "unknown", ""},
		{errFake("Get \"http://127.0.0.1:1/api/v1/assistant\": connection refused"), "unreachable", ""},
	} {
		if code, reason := sendFailure(c.err); code != c.code || reason != c.reason {
			t.Errorf("%v: %s %q", c.err, code, reason)
		}
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

// The user's own voice as the user-speech gate counts it (#5672): a stretch of spokeVoice by the audio's own length,
// gaps between words up to bargeGap bridged; nothing of it while the model's speech plays or within echoTail after;
// a stretch begun then counts from after it. Heard only since the agent time and by when the model acted.
func TestOwnVoice(t *testing.T) {
	const frame = 20 * time.Millisecond
	at := time.Now()
	// talk feeds d of frames from from on, voiced or not: whether one of them made it the user speaking.
	talk := func(o *ownVoice, from time.Time, d time.Duration, voiced bool, playEnd time.Time) bool {
		spoke := false
		for i := range int(d / frame) {
			if o.add(from.Add(time.Duration(i)*frame), voiced, frame, playEnd) {
				spoke = true
			}
		}
		return spoke
	}
	var o ownVoice
	if talk(&o, at, 100*time.Millisecond, true, time.Time{}) {
		t.Error("a click is the user speaking")
	}
	talk(&o, at.Add(100*time.Millisecond), 200*time.Millisecond, false, time.Time{}) // longer than bargeGap: it is forgotten
	if talk(&o, at.Add(300*time.Millisecond), 180*time.Millisecond, true, time.Time{}) {
		t.Error("the click and a short sound after a pause are taken together")
	}
	o = ownVoice{}
	if !talk(&o, at, 240*time.Millisecond, true, time.Time{}) {
		t.Error("one syllable is not the user speaking")
	}
	o = ownVoice{}
	talk(&o, at, 100*time.Millisecond, true, time.Time{})
	talk(&o, at.Add(100*time.Millisecond), 100*time.Millisecond, false, time.Time{}) // between two words
	if !talk(&o, at.Add(200*time.Millisecond), 100*time.Millisecond, true, time.Time{}) {
		t.Error("two words with a short gap are not the user speaking")
	}

	// The model's speech plays out at playEnd: its echo, however long, is not the user's voice.
	playEnd := at.Add(2 * time.Second)
	o = ownVoice{}
	if talk(&o, at, 2*time.Second+echoTail, true, playEnd) {
		t.Error("the model's speech coming back is the user speaking")
	}
	// A voice that goes on past echoTail counts from then on: 160 ms of it is not yet the user speaking, 200 ms is.
	o = ownVoice{}
	if talk(&o, playEnd.Add(echoTail-300*time.Millisecond), 460*time.Millisecond, true, playEnd) {
		t.Error("the voice within echoTail counted")
	}
	o = ownVoice{}
	if !talk(&o, playEnd.Add(echoTail-300*time.Millisecond), 500*time.Millisecond, true, playEnd) {
		t.Error("the voice after echoTail did not count")
	}

	g := speechGate{voice: at}
	for _, c := range []struct {
		agent, now time.Time
		want       bool
	}{
		{at.Add(-time.Second), at.Add(time.Second), true},
		{at, at.Add(time.Second), false},                         // not since the agent's words
		{at.Add(-time.Second), at.Add(-time.Millisecond), false}, // after the model acted
		{at.Add(-time.Minute), at.Add(gateWindow + time.Second), false},
	} {
		if got := g.heardSince(c.agent, c.now); got != c.want {
			t.Errorf("heardSince(%v, %v) = %v", c.agent.Sub(at), c.now.Sub(at), got)
		}
	}
	if (&speechGate{}).heardSince(time.Time{}, at) {
		t.Error("heard with no voice")
	}
}

// #5789: when the app's audio echoes the model (speechGate.echoes), a caption since the agent's words does not open
// the gate, a typed line does; otherwise a caption does.
func TestEchoCaptionGate(t *testing.T) {
	at := time.Now()
	agent, now := at.Add(-time.Second), at.Add(time.Second)
	for _, c := range []struct {
		name string
		g    speechGate
		want bool
	}{
		{"caption", speechGate{user: at}, true},
		{"caption, echoes", speechGate{user: at, echoes: true}, false},
		{"typed, echoes", speechGate{user: at, typed: at, echoes: true}, true},
		{"typed before the agent's words, echoes", speechGate{user: at, typed: agent.Add(-time.Second), echoes: true}, false},
	} {
		if got := c.g.openSince(agent, now); got != c.want {
			t.Errorf("%s: openSince = %v", c.name, got)
		}
	}
}

// #5904: what the gate's decision went by, for the log: the agent time from the handover and what moved it there (the
// last move, or the one before), and each of the user's caption, typed line and own voice from the handover with
// whether it counted and the first reason it did not.
func TestGateExplainsItsDecision(t *testing.T) {
	acted := time.Now()
	agent := acted.Add(-2 * time.Second)
	sec := func(n float64) time.Time { return acted.Add(time.Duration(n * float64(time.Second))) }
	moved := speechGate{}
	moved.told(agent, agentAcked)
	moved.told(sec(1), agentSent) // after the handover: it is held to the one before
	for _, c := range []struct {
		name string
		g    speechGate
		at   time.Time
		want string
	}{
		{"nothing", speechGate{}, time.Time{}, "from the handover: agent words none, caption none, typed line none, own voice none"},
		{"the agent time moved again", moved, agent, "agent words 2s before (acked), caption none"},
		{"counted", speechGate{agent: agent, agentBy: agentSent, caption: sec(-1), typed: sec(-0.5), voice: sec(-1.5)}, agent,
			"agent words 2s before (sent), caption 1s before (counted), typed line 500ms before (counted), own voice 1.5s before (counted)"},
		{"before the agent's words", speechGate{agent: agent, agentBy: agentReconnect, caption: sec(-3), typed: sec(-3), voice: sec(-3)}, agent,
			"agent words 2s before (reconnect), caption 3s before (not counted: not after the agent words), " +
				"typed line 3s before (not counted: not after the agent words), own voice 3s before (not counted: not after the agent words)"},
		{"no echo cancellation, old, late", speechGate{caption: sec(-1), typed: sec(-40), voice: sec(0.3), echoes: true}, time.Time{},
			"caption 1s before (not counted: no echo cancellation), typed line 40s before (not counted: over 30s old), " +
				"own voice 300ms after (not counted: after the handover)"},
		{"a caption after the handover", speechGate{agent: agent, agentBy: agentSent, caption: sec(0.4)}, agent, "caption 400ms after (counted)"},
	} {
		if got := c.g.explain(c.at, acted, sec(0.5), c.g.voice); !strings.Contains(got, c.want) {
			t.Errorf("%s: %s\nwant %s", c.name, got, c.want)
		}
	}
}

// sound sends the app's audio at amplitude amp (tone) for d, in 20 ms frames: at once, or in real time (paced).
func (a *app) sound(d time.Duration, amp int16, paced bool) {
	for range int(d / (20 * time.Millisecond)) {
		_ = a.c.Write(context.Background(), websocket.MessageBinary, tone(320, amp))
		if paced {
			time.Sleep(20 * time.Millisecond)
		}
	}
}
