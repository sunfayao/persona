package voice

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Protocol 2 (4.2–4.6, 4.10): a call bound to the conversation it was opened from.

// boundUpstream is a live-dialect service whose model hands every line the user types over: as a client delegation
// (dlg1, dlg2…), or with tools as a delegate_task call on it (c1, c2…). What the extension sends (it has an event id)
// is only recorded.
func boundUpstream(t *testing.T, tools bool) *fakeUpstream {
	var mu sync.Mutex
	n := 0
	return newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		case "session.commentary.append":
			if f["delegation_id"] != nil || f["event_id"] != nil {
				return
			}
			mu.Lock()
			n++
			id := n
			mu.Unlock()
			if !tools {
				delegate(u, c, fmt.Sprintf("dlg%d", id), content(f))
				return
			}
			u.send(c, map[string]any{"type": "session.delegation.created", "delegation": map[string]any{"id": fmt.Sprintf("dlg%d", id), "target": "client"}})
			u.send(c, map[string]any{"type": "response.event", "delegation_id": fmt.Sprintf("dlg%d", id), "event": map[string]any{
				"type": "function_call", "call_id": fmt.Sprintf("c%d", id), "name": "delegate_task", "arguments": fmt.Sprintf(`{"task":%q}`, content(f)),
			}})
		}
	})
}

// said matches what the extension says by itself (a commentary on no delegation, with its event id).
func said(f map[string]any) bool {
	return f["type"] == "session.commentary.append" && f["delegation_id"] == nil && f["event_id"] != nil
}

// boundStart is a protocol 2 start bound to session sid on machine m, with more fields.
func boundStart(sid string, more map[string]any) map[string]any {
	f := map[string]any{"type": "start", "protocol": 2, "target": map[string]any{"machine_id": "m", "session_id": sid}}
	for k, v := range more {
		f[k] = v
	}
	return f
}

// agentHub is a hub with a Claude Code session a1 on machine m, idle.
func agentHub(t *testing.T) *fakeHub {
	hub := newFakeHub(t)
	hub.sessions["a1"] = &fakeSession{id: "a1", status: "idle", look: func(s *proto.Session) {
		s.Agent, s.Title, s.Cwd = "claude", "修登录页的测试", "/home/me/web-api"
	}}
	return hub
}

// runStates reads the app's frames until run id is in state last, and returns the states it went through.
func (a *app) runStates(id, last string) []string {
	a.t.Helper()
	var states []string
	a.until(func(f map[string]any) bool {
		if f["type"] != "run" || f["id"] != id {
			return false
		}
		states = append(states, f["state"].(string))
		return f["state"] == last
	})
	return states
}

func runIn(state string) func(map[string]any) bool {
	return func(f map[string]any) bool { return f["type"] == "run" && f["state"] == state }
}

func TestBoundToTraeCLI(t *testing.T) {
	for _, test := range []struct{ label, want string }{
		{label: "TRAEX", want: "TRAE CLI"},
		{label: "Workspace agent", want: "Workspace agent"},
	} {
		t.Run(test.label, func(t *testing.T) {
			hub := agentHub(t)
			hub.sessions["a1"].look = func(s *proto.Session) {
				s.Agent, s.AgentLabel = "traex", test.label
			}
			up := boundUpstream(t, false)
			pl := testPlugin(t, liveProvider(up), hub.server(t))
			a := dialApp(t, pl, boundStart("a1", nil))
			target := a.until(ofType("ready"))["target"].(map[string]any)
			if target["agent_label"] != test.want {
				t.Errorf("ready target label = %q, want %q", target["agent_label"], test.want)
			}
		})
	}
}

// The target is checked before ready: gone, read-only, or bound by another extension are fatal with their code; an
// offline machine is not.
func TestBindingRefusals(t *testing.T) {
	hub := agentHub(t)
	hub.sessions["ro1"] = &fakeSession{id: "ro1", status: "idle", look: func(s *proto.Session) {
		s.SourceKind, s.Capabilities, s.ReadOnlyReason = "codex_history", &proto.SessionCapabilities{Read: true}, "只能同步历史"
	}}
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	for _, c := range []struct {
		name   string
		start  map[string]any
		header http.Header
		code   string
	}{
		{"gone", boundStart("nope", nil), nil, codeTargetGone},
		{"read only", boundStart("ro1", nil), nil, codeTargetReadOnly},
		{"another extension", boundStart("a1", nil), http.Header{"X-Persona-Role": {"extension"}}, codeTargetForbidden},
	} {
		var headers []http.Header
		if c.header != nil {
			headers = append(headers, c.header)
		}
		a := dialApp(t, pl, c.start, headers...)
		if f := a.until(ofType("error")); f["code"] != c.code || f["fatal"] != true {
			t.Errorf("%s: %v", c.name, f)
		}
	}
	a := dialApp(t, pl, map[string]any{"type": "start", "protocol": 2})
	if f := a.until(ofType("error")); f["fatal"] != true {
		t.Errorf("no target: %v", f)
	}

	hub.offline = true
	a = dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	if f := a.until(ofType("error")); f["code"] != codeMachineOffline || f["fatal"] != false {
		t.Fatalf("offline: %v", f)
	}
}

// Another extension asking for protocol 2 without a target gets protocol 1: the main conversation, its frames.
func TestExtensionCallerStaysOnProtocolOne(t *testing.T) {
	hub := newFakeHub(t)
	up := delegatingUpstream(t)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start", "protocol": 2}, http.Header{"X-Persona-Role": {"extension"}})
	if ready := a.until(ofType("ready")); ready["protocol"] != nil || ready["target"] != nil {
		t.Fatalf("ready: %v", ready)
	}
	a.send(map[string]any{"type": "text", "text": "明天有什么会"})
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "明天下午三点有会") {
		t.Fatalf("result: %s", got)
	}
	a.protocolOne()
}

// A dialog that is only a notice Enter closes is closed and the message sent again with the same id: nobody hears of
// it. Another dialog while the plan waits: refused, and the model is told the plan can take what was said as feedback.
func TestDialogRefusals(t *testing.T) {
	hub := agentHub(t)
	hub.onSend = func(m sentMsg) (int, string) {
		switch {
		case strings.HasPrefix(m.Text, "第三步别做"):
			return http.StatusConflict, `{"error":"电脑上的终端正开着一个对话框","code":"dialog_open","notice":false,"delivered":"none"}`
		case !m.Dismiss:
			return http.StatusConflict, `{"error":"电脑上的终端正开着一个对话框（「Auto mode」）","code":"dialog_open","notice":true,"dialog":"Auto mode","delivered":"none"}`
		}
		return 0, ""
	}
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "跑一下测试"})
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "has finished this turn") {
		t.Fatalf("result: %s", got)
	}
	if msgs := hub.messages(); len(msgs) != 1 || !msgs[0].Dismiss {
		t.Fatalf("sent %+v", msgs)
	}

	hub.mu.Lock()
	hub.cards = []proto.Interaction{{ID: "p1", MachineID: "m", SessionID: "a1", Kind: proto.IxPlan}}
	hub.mu.Unlock()
	a.send(map[string]any{"type": "text", "text": "第三步别做"})
	if f := a.until(runIn(runRefused)); f["code"] != "dialog_open" {
		t.Fatalf("refused: %v", f)
	}
	if got := content(up.nextMatch(on("dlg2"))); !strings.Contains(got, "as feedback on the plan") {
		t.Fatalf("told: %s", got)
	}
}

// A send that broke off is unknown: not folded, not sent again.
func TestBoundSendThatBreaksOffIsUnknown(t *testing.T) {
	hub := agentHub(t)
	hub.cut = true
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "跑一下测试"})
	if f := a.until(runIn(runUnknown)); f["code"] != "unknown" {
		t.Fatalf("run: %v", f)
	}
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "reached Claude Code") {
		t.Fatalf("told: %s", got)
	}
}

// While a card waits, a word that only approves or refuses is not sent: cards are tapped.
func TestApprovalWordIsNotSent(t *testing.T) {
	hub := agentHub(t)
	hub.cards = []proto.Interaction{{ID: "x1", MachineID: "m", SessionID: "a1", Kind: proto.IxPermission, ToolName: "Bash"}}
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "批准"})
	if f := a.until(runIn(runRefused)); f["code"] != "on_screen" {
		t.Fatalf("run: %v", f)
	}
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "tapping them on the screen") {
		t.Fatalf("told: %s", got)
	}
	a.send(map[string]any{"type": "text", "text": "回答：第二个"})
	if m := hub.waitSent(1); !strings.HasPrefix(m.Text, "第二个\n") {
		t.Fatalf("sent %+v", m)
	}
}

// The marks are read however the model writes them: quoted as the instructions write them, with a space before the
// colon, in either case; a word that only starts like one is not one.
func TestSplitMark(t *testing.T) {
	for _, c := range []struct{ in, kind, rest string }{
		{"状态：到哪了", "status", "到哪了"},
		{"「状态：到哪了」", "status", "到哪了"},
		{"「状态」：到哪了", "status", "到哪了"},
		{"『停止：』", "stop", ""},
		{`"Stop: the tests"`, "stop", "the tests"},
		{"“Status : how far”", "status", "how far"},
		{"status: x", "status", "x"},
		{"状況：どう", "status", "どう"},
		{"回答 ：第二个", "answer", "第二个"},
		{"「回答：第二个」", "answer", "第二个"},
		{`回答："第二个"`, "answer", `"第二个"`},
		{"stopwatch: x", "", "stopwatch: x"},
		{"停止测试", "", "停止测试"},
		{"\"修一下测试\"", "", "\"修一下测试\""},
		{"Stop", "", "Stop"},
	} {
		if kind, rest := splitMark(c.in); kind != c.kind || rest != c.rest {
			t.Errorf("%q: %q %q, want %q %q", c.in, kind, rest, c.kind, c.rest)
		}
	}
}

// Bound to the main conversation with no recent turns from the app: the snapshot is read from the hub (its last
// turns, a card's question, the agents); the question is asked and answered by voice (asks.go), and a handover after
// it is voice/delegated.
func TestBoundToTheMainConversation(t *testing.T) {
	hub := newFakeHub(t)
	hub.add("main1", proto.Item{Role: "user", Kind: proto.KindText, Text: "明天有什么会"},
		proto.Item{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"},
		proto.Item{Role: "assistant", Kind: proto.KindText, Text: "明天下午三点有个评审会。"})
	hub.sessions["a1"] = &fakeSession{id: "a1", status: "working", look: func(s *proto.Session) { s.Title = "web-api" }}
	hub.cards = []proto.Interaction{{ID: "q1", MachineID: "m", SessionID: "main1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个数据库？"}}}}
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("main1", nil))
	if ready := a.until(ofType("ready")); ready["target"].(map[string]any)["kind"] != "main" {
		t.Fatalf("ready: %v", ready)
	}
	instructions := up.next("session.start")["session"].(map[string]any)["instructions"].(string)
	for _, want := range []string{"The Persona main conversation: the full assistant running on the hub's computer", "the user: 明天有什么会", "(Main conversation used Bash)",
		"Main conversation: 明天下午三点有个评审会。", "- A question: 用哪个数据库？", "The agents on each computer:\n- web-api · working"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions lack %q: %s", want, instructions)
		}
	}
	if got := content(up.nextMatch(saying("用哪个数据库"))); !strings.Contains(got, "The Persona main conversation has stopped to wait for the user to answer a question") {
		t.Fatalf("asked: %s", got)
	}
	a.send(map[string]any{"type": "text", "text": "Postgres"})
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "The user's answer went to the Persona main conversation") {
		t.Fatalf("answered: %s", got)
	}
	if res := hub.resolutions(); len(res) != 1 || res[0].Res.Answers["用哪个数据库？"] != "Postgres" {
		t.Fatalf("resolved %+v", res)
	}
	a.send(map[string]any{"type": "text", "text": "查一下邮件"})
	if m := hub.waitSent(1); m.Session != "main1" || !strings.Contains(m.Text, "Keep the reply short") {
		t.Fatalf("sent %+v", m)
	}
	if got := content(up.nextMatch(on("dlg2"))); !strings.Contains(got, "The Persona main conversation has finished this turn") {
		t.Fatalf("result: %s", got)
	}
}

// The upstream drops: protocol 2 says so with link frames; three drops in a row end the call (upstream_failed), but a
// session that held stableAfter starts the count over.
func TestReconnectsStartOverAfterAStableSession(t *testing.T) {
	for _, stable := range []bool{false, true} {
		t.Run(fmt.Sprint("stable=", stable), func(t *testing.T) {
			setVar(t, &stableAfter, time.Hour)
			if stable {
				setVar(t, &stableAfter, 0)
			}
			hub := agentHub(t)
			var sessions atomic.Int32
			up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
				if f["type"] != "session.start" {
					return
				}
				u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
				if sessions.Add(1) <= 5 {
					_ = c.Close(websocket.StatusGoingAway, "upstream restarts")
				}
			})
			pl := testPlugin(t, liveProvider(up), hub.server(t))
			a := dialApp(t, pl, boundStart("a1", nil))
			a.until(ofType("ready"))
			a.until(func(f map[string]any) bool { return f["type"] == "link" && f["state"] == "reconnecting" })
			a.until(func(f map[string]any) bool { return f["type"] == "link" && f["state"] == "live" })
			if !stable {
				if f := a.until(ofType("error")); f["code"] != codeUpstreamFailed || f["fatal"] != true {
					t.Fatalf("error: %v", f)
				}
				return
			}
			waitFor(t, "the sixth session", func() bool { return sessions.Load() >= 6 })
			time.Sleep(200 * time.Millisecond)
			a.mu.Lock()
			failed := a.types["error"]
			a.mu.Unlock()
			if failed {
				t.Fatal("the call ended")
			}
		})
	}
}

// A service whose model's backend is down may start the session and only then say so (error upstream_unavailable,
// then a 1008 close): the reconnects go as for any drop, and when they run out the call ends as the service being
// unavailable with its words, not as a dropped connection. A run of drops whose last one the service did not explain
// stays upstream_failed (TestReconnectsStartOverAfterAStableSession).
func TestServiceDownAfterTheSessionStartedEndsAsUnavailable(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprint("bound=", bound), func(t *testing.T) {
			setVar(t, &stableAfter, time.Hour)
			hub := agentHub(t)
			var sessions atomic.Int32
			up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
				if f["type"] != "session.start" {
					return
				}
				sessions.Add(1)
				u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
				u.send(c, map[string]any{"type": "error", "error": map[string]any{"type": "server_error",
					"code": "upstream_unavailable", "message": "backend down (cluster c1)"}})
				_ = c.Close(websocket.StatusPolicyViolation, "upstream unavailable")
			})
			pl := testPlugin(t, liveProvider(up), hub.server(t))
			start := map[string]any{"type": "start"}
			if bound {
				start = boundStart("a1", nil)
			}
			a := dialApp(t, pl, start)
			a.until(ofType("ready"))
			f := a.until(ofType("error"))
			if f["code"] != codeUpstreamUnavailable || f["fatal"] != true {
				t.Fatalf("error: %v", f)
			}
			if msg, _ := f["message"].(string); !strings.Contains(msg, "backend down (cluster c1)") {
				t.Fatalf("the service's words are not in the message: %q", msg)
			}
			if n := sessions.Load(); n != maxReconnects+1 {
				t.Fatalf("%d sessions, want %d", n, maxReconnects+1)
			}
		})
	}
}

// The insertion gate: the model's audio plays out (its play end added up from the audio's length), the user's voice
// in the app's audio holds what is said, a delegation holds it a while; a delegation's own answer waits for the user
// and for the model's speech to have played out. The user's voice holds nothing past answerHoldMost / sayHoldMost.
func TestPacing(t *testing.T) {
	now := time.Now()
	var p pacing
	p.played(now, 32000, 16000)                           // 1 s of audio
	p.played(now.Add(100*time.Millisecond), 16000, 16000) // half a second more, after it
	if want := now.Add(1500 * time.Millisecond); !p.playEnd.Equal(want) {
		t.Fatalf("play end %v, want %v", p.playEnd.Sub(now), want.Sub(now))
	}
	p.delegated, p.voice = now, now.Add(200*time.Millisecond)
	if got, want := p.readyAt(true, now), latest(now.Add(1500*time.Millisecond+speechQuiet), now.Add(delegationQuiet), now.Add(200*time.Millisecond+userQuiet)); !got.Equal(want) {
		t.Fatalf("a say at %v, want %v", got.Sub(now), want.Sub(now))
	}
	if got := p.readyAt(false, now); !got.Equal(now.Add(1500 * time.Millisecond)) {
		t.Fatalf("an answer at %v (the model speaks until 1.5 s)", got.Sub(now))
	}
	p.playEnd = now
	if got := p.readyAt(false, now); !got.Equal(latest(now.Add(200*time.Millisecond+userQuiet), now.Add(replyAudioIn))) {
		t.Fatalf("an answer at %v", got.Sub(now))
	}
	p.voice = time.Time{} // an answer ready at once waits for the audio of the reply the model handed over in
	if got := p.readyAt(false, now); !got.Equal(now.Add(replyAudioIn)) {
		t.Fatalf("an answer right after the handover at %v", got.Sub(now))
	}

	// The user's voice goes on (or what reads as it): an answer goes answerHoldMost after it was queued, a say
	// sayHoldMost after, once the model is quiet.
	p.voice = now.Add(time.Hour)
	if got := p.readyAt(false, now); !got.Equal(now.Add(answerHoldMost)) {
		t.Fatalf("an answer under endless voice at %v", got.Sub(now))
	}
	if got := p.readyAt(true, now); !got.Equal(now.Add(sayHoldMost)) {
		t.Fatalf("a say under endless voice at %v", got.Sub(now))
	}
	p.playEnd = now.Add(time.Minute) // the model reads a long answer: that still holds both
	if got := p.readyAt(false, now); !got.Equal(p.playEnd) {
		t.Fatalf("an answer while the model speaks at %v", got.Sub(now))
	}
	if got := p.readyAt(true, now); !got.Equal(p.playEnd.Add(speechQuiet)) {
		t.Fatalf("a say while the model speaks at %v", got.Sub(now))
	}
}

// A frame has a voice in it over voiceRMS, and over the noise floor: a steady noise over voiceRMS (a TV, a car) stops
// counting once it filled floorSeconds, a voice over it still counts; a gap in the audio starts the floor over.
func TestNoiseFloor(t *testing.T) {
	quiet, noise, voice := meanSquare(tone(320, 100)), meanSquare(tone(320, 400)), meanSquare(tone(320, 3000))
	if meanSquare(make([]byte, 640)) != 0 || meanSquare(nil) != 0 {
		t.Fatal("silence is not 0")
	}
	var p pacing
	at := time.Unix(1_800_000_000, 0)
	frames := func(from time.Duration, d time.Duration, ms float64) (heard int) {
		for o := time.Duration(0); o < d; o += 20 * time.Millisecond {
			if p.heard(at.Add(from+o), ms) {
				heard++
			}
		}
		return heard
	}
	if frames(0, 3*time.Second, quiet) != 0 {
		t.Fatal("the quiet room has a voice in it")
	}
	if frames(3*time.Second, time.Second, voice) != 50 {
		t.Fatal("a voice is not heard")
	}
	// A noise over voiceRMS: heard until it is the quietest of floorSeconds, then not.
	if n := frames(4*time.Second, floorSeconds*time.Second, noise); n == 0 || n == floorSeconds*50 {
		t.Fatalf("the noise was heard in %d frames", n)
	}
	if n := frames((4+floorSeconds)*time.Second, 3*time.Second, noise); n != 0 {
		t.Fatalf("the steady noise is heard (%d frames)", n)
	}
	if n := frames((7+floorSeconds)*time.Second, time.Second, voice); n != 50 {
		t.Fatalf("a voice over the noise is heard in %d frames", n)
	}
	// Two seconds without audio: the floor is not known, voiceRMS alone again.
	if !p.heard(at.Add((10+floorSeconds)*time.Second), noise) {
		t.Fatal("after a gap the noise is not heard")
	}
}

// tone is n samples of PCM16 at amplitude amp.
func tone(n int, amp int16) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		v := amp
		if i%2 == 1 {
			v = -amp
		}
		b[2*i], b[2*i+1] = byte(uint16(v)), byte(uint16(v)>>8)
	}
	return b
}
