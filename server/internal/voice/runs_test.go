package voice

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The protocol 1 scenarios of docs/assistant/voice.md "Server tests": a run is the turn its message started, read whole; the gate,
// the tools by role, the fake dialect.

// delegatingUpstream is a live-dialect service whose model hands every line the user types over as a client
// delegation (dlg1, dlg2…). What the extension says (a report, a refusal: a commentary with no delegation that names
// the Persona main conversation) is only recorded.
func delegatingUpstream(t *testing.T) *fakeUpstream { return delegatingUpstreamWith(t, nil) }

// delegatingUpstreamWith is delegatingUpstream that also hands what the extension sends on a delegation to onAnswer.
func delegatingUpstreamWith(t *testing.T, onAnswer func(u *fakeUpstream, c *websocket.Conn, f map[string]any)) *fakeUpstream {
	var mu sync.Mutex
	n := 0
	return newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		case "session.commentary.append":
			content, _ := f["content"].(string)
			if f["delegation_id"] != nil && onAnswer != nil {
				onAnswer(u, c, f)
			}
			if f["delegation_id"] != nil || strings.Contains(content, "Persona") {
				return
			}
			mu.Lock()
			n++
			id := "dlg" + string(rune('0'+n))
			mu.Unlock()
			u.send(c, map[string]any{"type": "session.delegation.created", "delegation": map[string]any{
				"id": id, "target": "client", "content": []map[string]string{{"type": "input_text", "text": content}},
			}})
		}
	})
}

// on matches the service's commentary on a delegation ("" : one on none, something said).
func on(delegation string) (string, func(map[string]any) bool) {
	return "a commentary on " + delegation, func(f map[string]any) bool {
		if f["type"] != "session.commentary.append" {
			return false
		}
		if delegation == "" {
			content, _ := f["content"].(string)
			return f["delegation_id"] == nil && strings.Contains(content, "Persona")
		}
		return f["delegation_id"] == delegation
	}
}

func content(f map[string]any) string { s, _ := f["content"].(string); return s }

// waitFor waits up to 5 s for cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func setVar[T any](t *testing.T, v *T, to T) {
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

// liveProvider is a live-dialect upstream without function tools: its model hands work over as plain delegations.
func liveProvider(up *fakeUpstream) Config {
	return Config{Providers: []Provider{{ID: "rt", URL: up.url(), Models: []string{"m1"}, Tools: new(false)}}}
}

// A long answer, cut in pages and with an item cut for pages, reaches the model whole, made speakable: code and links
// become stand-ins, the end of the answer is there.
func TestLongAnswerIsSpokenWhole(t *testing.T) {
	hub := newFakeHub(t)
	hub.manual, hub.pageAt = true, 1
	up := delegatingUpstream(t)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "修一下登录页的测试"})
	m := hub.waitSent(1)
	hub.status("main1", "working")
	hub.add("main1", proto.Item{Role: "assistant", Kind: proto.KindToolUse, Text: "Bash"},
		proto.Item{ID: "long", Role: "assistant", Kind: proto.KindText, Text: "修好了，改了 `web/src/Login.tsx`：…（已截断）", FullSize: 4000})
	whole := "修好了，改了 `web/src/Login.tsx`：\n\n```ts\nexpect(login()).toBe(true)\nexpect(x).toBe(1)\n```\n\n详情 https://example.com/ci/7 。"
	hub.mu.Lock()
	hub.full["long"] = whole
	hub.mu.Unlock()
	hub.add("main1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "最后：测试全部通过。"})
	hub.end("main1")
	got := content(up.nextMatch(on("dlg1")))
	for _, want := range []string{"the file Login.tsx", "(a code block of 2 lines, on the screen)", "(a link)", "最后：测试全部通过。"} {
		if !strings.Contains(got, want) {
			t.Errorf("result lacks %q: %s", want, got)
		}
	}
	for _, not := range []string{"expect(", "https://", "the middle is left out"} {
		if strings.Contains(got, not) {
			t.Errorf("result reads out %q: %s", not, got)
		}
	}
	if !strings.Contains(m.Text, "修一下登录页的测试") {
		t.Fatalf("sent %q", m.Text)
	}
}

// A second instruction the hub refuses is not folded: the first is answered on its own, the second told why it was
// not sent.
func TestARefusedSendDoesNotHoldTheFirstBack(t *testing.T) {
	hub := newFakeHub(t)
	hub.manual = true
	up := delegatingUpstream(t)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "查明天的日程"})
	hub.waitSent(1)
	hub.status("main1", "working")
	hub.mu.Lock()
	hub.onSend = func(sentMsg) (int, string) {
		return http.StatusConflict, `{"error":"Claude 在等你先处理上面的卡片","code":"dialog_open"}`
	}
	hub.mu.Unlock()
	a.send(map[string]any{"type": "text", "text": "顺便看看邮件"})
	refused := content(up.nextMatch(on("dlg2")))
	if !strings.Contains(refused, "was not sent to the Persona main conversation") || !strings.Contains(refused, "在等你先处理上面的卡片") {
		t.Fatalf("refusal: %s", refused)
	}
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["id"] == "d2" && f["done"] == true })
	hub.add("main1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "明天下午三点有会"})
	hub.end("main1")
	got := content(up.nextMatch(on("dlg1")))
	if !strings.Contains(got, "明天下午三点有会") || strings.Contains(got, "顺便看看邮件") {
		t.Fatalf("result: %s", got)
	}
}

// A turn that leaves background work is answered now, saying so, and again when that work's turn ends (said: the
// delegation had its one commentary).
func TestHeldRunAnswersTwice(t *testing.T) {
	hub := newFakeHub(t)
	hub.manual = true
	up := delegatingUpstream(t)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.send(map[string]any{"type": "text", "text": "跑一下全部测试"})
	hub.waitSent(1)
	hub.status("main1", "working")
	hub.add("main1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "测试在后台跑了，先告诉你单元测试都过了。"})
	hub.edit("main1", func(s *fakeSession) { s.background = 1 })
	hub.end("main1")
	first := content(up.nextMatch(on("dlg1")))
	if !strings.Contains(first, "单元测试都过了") || !strings.Contains(first, "1 background task is still working") {
		t.Fatalf("first answer: %s", first)
	}
	hub.status("main1", "working")
	hub.add("main1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "集成测试也都过了。"})
	hub.edit("main1", func(s *fakeSession) { s.background = 0 })
	hub.end("main1")
	again := content(up.nextMatch(on("")))
	if !strings.Contains(again, "background work has finished") || !strings.Contains(again, "集成测试也都过了") || strings.Contains(again, "单元测试") {
		t.Fatalf("second answer: %s", again)
	}
}

// Another extension calling (X-Persona-Role: extension) gets ask_persona only, from an upstream that takes tools: it may
// hand work to the main conversation, and the measured service hands nothing over without a
// tool; the knowledge base and the agents are not its to reach.
func TestExtensionCallerGetsOnlyAskPersona(t *testing.T) {
	hub := newFakeHub(t)
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		if f["type"] == "session.start" {
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		}
	})
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "rt", URL: up.url(), Models: []string{"m1"}, Tools: new(true)}}}, hub.server(t))
	for role, want := range map[string][]string{"extension": {"ask_persona"}, "full": toolNames, "": toolNames} {
		a := dialApp(t, pl, map[string]any{"type": "start"}, http.Header{"X-Persona-Role": {role}})
		a.until(ofType("ready"))
		start := up.next("session.start")["session"].(map[string]any)
		decls, _ := start["tools"].([]any)
		var got []string
		for _, d := range decls {
			got = append(got, d.(map[string]any)["name"].(string))
		}
		instructions := start["instructions"].(string)
		if strings.Join(got, ",") != strings.Join(want, ",") || strings.Contains(instructions, "send_to_agent") != (len(want) > 1) ||
			!strings.Contains(instructions, "ask_persona") {
			t.Errorf("role %q: tools %v", role, got)
		}
		_ = a.c.CloseNow()
	}
}

// The fake dialect: no service; a typed line is handed over as it is, what the model would be told comes back as its
// words.
func TestFakeDialect(t *testing.T) {
	hub := newFakeHub(t)
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "dev", Dialect: DialectFake}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	if ready := a.until(ofType("ready")); ready["model"] != "dev/fake" {
		t.Fatalf("ready: %v", ready)
	}
	a.send(map[string]any{"type": "text", "text": "查明天的日程"})
	a.until(finalLine("user", "查明天的日程"))
	line := a.until(func(f map[string]any) bool {
		return f["type"] == "transcript" && f["role"] == "assistant" && f["final"] == true
	})
	if text, _ := line["text"].(string); !strings.Contains(text, "<agent_output>\n明天下午三点有会\n</agent_output>") {
		t.Fatalf("the model's line: %v", line)
	}
	if msgs := hub.messages(); len(msgs) != 1 || !strings.HasPrefix(msgs[0].Text, "查明天的日程\n") {
		t.Fatalf("sent %+v", msgs)
	}
	a.protocolOne()
}

// Message ids are random: a restarted extension, or a second call, never reuses one the hub already sent.
func TestVoiceMessageIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := voiceMessageID()
		if !isVoiceID(id) || seen[id] {
			t.Fatalf("id %q", id)
		}
		seen[id] = true
	}
}
