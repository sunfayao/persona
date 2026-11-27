package node

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// shellPane is a herdr pane entry with no agent in it, marked as Persona's for session (none when "").
func shellPane(pane, tab, ws, cwd, session string) map[string]any {
	p := map[string]any{"pane_id": pane, "tab_id": tab, "workspace_id": ws, "cwd": cwd}
	if session != "" {
		p["tokens"] = map[string]any{personaTabToken: session}
	}
	return p
}

// quitsOn makes herdr's snapshot lose agent once Ctrl+C has been pressed twice in its pane, as claude exiting to its
// shell does: the pane stays, holding the shell (marked as Persona's for the session).
func quitsOn(f *fakeHerdr, pane, session string, still []any, gone map[string]any) {
	var mu sync.Mutex
	presses := 0
	f.after(func(c herdrCall) {
		if c.Method != "pane.send_keys" || str(c.Params["pane_id"]) != pane {
			return
		}
		mu.Lock()
		presses++
		n := presses
		mu.Unlock()
		if n == 2 {
			f.reply("session.snapshot", snapshot(nil, nil, still, []any{gone}))
		}
	})
}

// closesNothing fails when a trace has herdr close a tab or a pane.
func closesNothing(t *testing.T, f *fakeHerdr) {
	t.Helper()
	if got := f.trace("tab.close", "pane.close"); len(got) != 0 {
		t.Errorf("closed the agent's terminal: %q", got)
	}
}

// session.stop quits a running agent with Ctrl+C twice and keeps its tab: the pane stays with the shell the agent
// ran in (#4754). A working agent is stopped too (a cancelled task's), unless retire asks for a still one only.
func TestHerdrStopKeepsTheTerminal(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	writeTranscript(t, home, cwd, sidA, time.Now().Add(-time.Hour), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`)
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd})
	working := claudeAgent(sidA, proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "")
	f.reply("session.snapshot", snapshot(nil, nil, []any{working}, nil))
	listSessions(t, n)
	waitCalls(t, f, "pane.report_agent_session", 1)
	f.reset()

	_, err := request(t, n, "session.stop", map[string]any{"session_id": sidA, "retire": true})
	if code, _ := refusalOf(t, err); code != notRetirable {
		t.Fatalf("retire stopped a working agent: %v", err)
	}
	quitsOn(f, "w1:p1", sidA, nil, shellPane("w1:p1", "w1:t1", "w1", cwd, sidA))
	res, err := request(t, n, "session.stop", map[string]any{"session_id": sidA})
	if err != nil || res["stopped"] != true {
		t.Fatalf("stop = %v, %v", res, err)
	}
	if got := f.trace("pane.send_keys"); len(got) != 2 {
		t.Errorf("keys %q", got)
	}
	closesNothing(t, f)
	if st, _ := n.state.get(sidA); st.Launcher != "cc" || st.Cwd != cwd {
		t.Errorf("state after stop = %+v", st)
	}

	res, err = request(t, n, "session.stop", map[string]any{"session_id": sidA})
	if err != nil || res["stopped"] != false {
		t.Fatalf("not running: %v, %v", res, err)
	}
}

// /exit (or /quit) sent to a running agent stops it as session.stop does, its tab kept, instead of typing it in; sent
// to an agent that is not running it does nothing (no tab is opened to resume it).
func TestHerdrSendExitStopsTheAgent(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	writeTranscript(t, home, cwd, sidA, time.Now().Add(-time.Hour), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`)
	writeTranscript(t, home, cwd, sidB, time.Now().Add(-time.Hour), `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"hi"},"uuid":"u1"}`)
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd})
	n.state.put(sidB, sessionState{Launcher: "cc", Cwd: cwd})
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, ""),
	}, nil))
	listSessions(t, n)
	waitCalls(t, f, "pane.report_agent_session", 1)
	f.reset()
	quitsOn(f, "w1:p1", sidA, nil, shellPane("w1:p1", "w1:t1", "w1", cwd, sidA))

	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": " /exit\n"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, withoutAgentReports(f.trace("pane.send_keys", "pane.send_input", "agent.prompt", "pane.send_text", "tab.close", "pane.close")), []string{
		sendKeys("w1:p1", "ctrl+c"), sendKeys("w1:p1", "ctrl+c"),
	})

	f.reset()
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidB, "text": "/quit"}); err != nil {
		t.Fatal(err)
	}
	if got := f.trace("tab.create", "workspace.create", "pane.send_input", "agent.prompt"); len(got) != 0 {
		t.Errorf("an agent that is not running was resumed to quit: %q", got)
	}
}

// Nothing closes a Persona tab left with only its shell any more (#4754): the ten-minute sweep of #851 is gone, so a
// node that serves for a while never closes one.
func TestPersonaTabsLeftWithAShellStay(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	n.state.put(sidA, sessionState{Launcher: "cc", Cwd: cwd})
	f.reply("session.snapshot", snapshot(nil, nil, nil, []any{shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)}))
	f.setScreen("w1:p1", "$ \nResume this session with: claude --resume "+sidA+"\n$ ")
	listSessions(t, n)
	if err := n.refreshHosts(t.Context()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // sleep-ok: an asynchronous close would show by then; nothing signals that none comes
	closesNothing(t, f)
}
