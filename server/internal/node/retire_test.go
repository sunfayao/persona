package node

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Retiring a finished task's agent and resuming it on the next message (retire.go).

// withoutAgentReports drops herdr's agent reports from a trace. The node reports a pane's agent — its state, and which
// session runs in it — in the background (herdrevents.go, herdrresume.go), so one can land between any two calls of
// whatever is being driven; a test that lists the calls it expects should not depend on where it fell (#1940).
func withoutAgentReports(trace []string) []string {
	return slices.DeleteFunc(slices.Clone(trace), func(c string) bool {
		return strings.HasPrefix(c, "pane.report_agent") || strings.HasPrefix(c, "pane.clear_agent_authority")
	})
}

// session.retire (what a hub before #4754 asks for a finished or idle agent) stops a still Claude and keeps its pane,
// with what it was started with, and refuses one that works or waits; one not running is no error.
func TestHerdrRetire(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	for _, id := range []string{sidA, sidB, sidC} {
		// A finished turn: a transcript with a prompt and nothing after it, written just now, is a turn under way
		// whatever herdr says (claudeStatus, #2069).
		writeTranscript(t, home, cwd, id, time.Now(), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`,
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"uuid":"a1","parentUuid":"u1"}`,
			`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","durationMs":1000}`)
	}
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd, Model: "opus"})
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidA, proto.StatusDone, "w1:p1", "w1:t1", "w1", cwd, ""),
		claudeAgent(sidB, proto.StatusWorking, "w1:p2", "w1:t2", "w1", cwd, ""),
		claudeAgent(sidC, proto.StatusBlocked, "w1:p3", "w1:t3", "w1", cwd, ""),
		claudeAgent(sidD, proto.StatusIdle, "w1:p4", "w1:t4", "w1", cwd, ""), // no transcript
	}, nil))
	listSessions(t, n)
	// sidA was started by Persona: the refresh gives herdr its resume command in the background (backfillResume),
	// once. Wait for it, or it lands among retire's calls.
	waitCalls(t, f, "pane.report_agent_session", 1)
	f.reset()

	for _, id := range []string{sidB, sidC} {
		_, err := request(t, n, "session.retire", map[string]any{"session_id": id})
		if code, _ := refusalOf(t, err); code != notRetirable {
			t.Errorf("%s: code %q", id, code)
		}
	}
	quitsOn(f, "w1:p1", sidA, []any{
		claudeAgent(sidB, proto.StatusWorking, "w1:p2", "w1:t2", "w1", cwd, ""),
		claudeAgent(sidC, proto.StatusBlocked, "w1:p3", "w1:t3", "w1", cwd, ""),
		claudeAgent(sidD, proto.StatusIdle, "w1:p4", "w1:t4", "w1", cwd, ""),
	}, shellPane("w1:p1", "w1:t1", "w1", cwd, sidA))
	res, err := request(t, n, "session.retire", map[string]any{"session_id": sidA})
	if err != nil || res["closed"] != true {
		t.Fatalf("retire = %v, %v", res, err)
	}
	if st, _ := n.state.get(sidA); st.Launcher != "cc" || st.Cwd != cwd || st.Model != "opus" {
		t.Errorf("state after retire = %+v", st)
	}
	assertTrace(t, withoutAgentReports(f.trace("pane.send_keys", "tab.close", "pane.close")), []string{
		sendKeys("w1:p1", "ctrl+c"), sendKeys("w1:p1", "ctrl+c"),
	})

	f.reset()
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	const gone = "0d000000-0000-4000-8000-0000000000ee"
	res, err = request(t, n, "session.retire", map[string]any{"session_id": gone})
	if err != nil || res["closed"] != false {
		t.Fatalf("not running: %v, %v", res, err)
	}
}

// session.retire stops an idle agy in its pane with the same two Ctrl+C presses and keeps the pane (#11087); a working
// one is refused, and a work agent the node cannot quit that way (cursor) is never stopped.
func TestHerdrRetireAgy(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	cwd := mkdir(t, filepath.Join(home, "proj"))
	working := agyTestAgent("w1:p2", cwd, agyOtherTestID)
	working["agent_status"] = proto.StatusWorking
	working["tab_id"] = "w1:t2"
	cursor := agentOf(cursorAgent, "", proto.StatusIdle, "w1:p3", "w1:t3", "w1", cwd, "cursor")
	seedAgents(t, n, f, agyTestAgent(agyTestPane, cwd, agyTestID), working, cursor)
	idOf := func(pane string) string {
		n.mu.Lock()
		defer n.mu.Unlock()
		for id, a := range n.agents {
			if a.PaneID == pane {
				return id
			}
		}
		t.Fatalf("no agent in %s", pane)
		return ""
	}
	idle, busy, other := idOf(agyTestPane), idOf("w1:p2"), idOf("w1:p3")

	// The agy rows are pane-derived (otherSession), never a hex session id: still accepted while agy runs in the pane.
	if idle != otherSession(host.Agent{Agent: agyAgent, PaneID: agyTestPane}) {
		t.Fatalf("agy row id = %q", idle)
	}
	_, err := request(t, n, "session.retire", map[string]any{"session_id": busy})
	if code, _ := refusalOf(t, err); code != notRetirable {
		t.Errorf("working agy: code %q", code)
	}
	if _, err := request(t, n, "session.retire", map[string]any{"session_id": other}); err == nil {
		t.Errorf("cursor: retired")
	}
	if got := withoutAgentReports(f.trace("pane.send_keys")); len(got) != 0 {
		t.Fatalf("keys sent before the idle agy's retire: %v", got)
	}
	quitsOn(f, agyTestPane, idle, []any{working, cursor}, shellPane(agyTestPane, "w1:t1", "w1", cwd, idle))
	res, err := request(t, n, "session.retire", map[string]any{"session_id": idle})
	if err != nil || res["closed"] != true {
		t.Fatalf("retire = %v, %v", res, err)
	}
	assertTrace(t, withoutAgentReports(f.trace("pane.send_keys", "tab.close", "pane.close")), []string{
		sendKeys(agyTestPane, "ctrl+c"), sendKeys(agyTestPane, "ctrl+c"),
	})
}
