package node

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// pi trusts the directory the start chose for that run only, on the first start and when herdr resumes it, whatever
// launcher runs it; cursor-agent and agy get no pi flag (#9462).
func TestWorkAgentCommandApprovesPiProjectFiles(t *testing.T) {
	for _, tc := range []struct{ launcher, program, model, want string }{
		{"pi", "pi", "", "pi --approve"},
		{"pi", "pi", "ag-chat/cursor/grok-4.7", "pi --model ag-chat/cursor/grok-4.7 --approve"},
		{"my-pi", "pi", "", "my-pi --approve"},
		{"cursor", "cursor-agent", "cursor/grok-4.7", "cursor-agent --model cursor/grok-4.7"},
		{"agy", "agy", "", "agy"},
	} {
		if got := workAgentCommand(tc.launcher, tc.program, tc.model, "", ""); got != tc.want {
			t.Errorf("workAgentCommand(%q, %q, %q) = %q, want %q", tc.launcher, tc.program, tc.model, got, tc.want)
		}
	}
	if got, want := workAgentResume(piAgent, workAgentCommand("pi", "pi", "", "", ""), "/s/a.jsonl"), `pi --approve --session /s/a.jsonl`; got != want {
		t.Errorf("resume = %q, want %q", got, want)
	}
}

func newWorkAgentTestTab(t *testing.T) (n *Node, f *fakeHerdr, home string, created *host.Created) {
	t.Helper()
	n, f, home = herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	created, err := n.newTab(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), home, "w1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return n, f, home, created
}

func TestReportedWorkAgentRejectsDuplicateRawRows(t *testing.T) {
	n, _, _ := herdrNode(t)
	first := host.Agent{Agent: cursorAgent, PaneID: "w1:p2", AgentStatus: proto.StatusIdle}
	second := host.Agent{Agent: cursorAgent, PaneID: "w1:p2", AgentStatus: proto.StatusIdle}
	n.mu.Lock()
	n.hostAgents = map[string][]host.Agent{host.Herdr: {first, second}}
	n.mu.Unlock()
	if id, ambiguous := n.reportedWorkAgent("w1:p2", cursorAgent); !ambiguous || id != "" {
		t.Fatalf("duplicate raw rows were treated as one agent: id %q ambiguous %v", id, ambiguous)
	}
}

func TestAbandonStartStopsKeysWhenRefreshFails(t *testing.T) {
	n, f, home, created := newWorkAgentTestTab(t)
	running := agentOf(cursorAgent, "", proto.StatusIdle, "w1:p2", "w1:t2", "w1", home, cursorAgent)
	f.reply("session.snapshot", snapshot(nil, nil, []any{running}, nil))
	presses := 0
	f.after(func(c herdrCall) {
		if c.Method != "pane.send_keys" || str(c.Params["pane_id"]) != "w1:p2" {
			return
		}
		presses++
		if presses == 2 {
			f.handle("session.snapshot", func(map[string]any) any {
				return herdrFail{Code: "unavailable", Message: "host went away"}
			})
			f.handle("agent.list", func(map[string]any) any {
				return herdrFail{Code: "unavailable", Message: "host went away"}
			})
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.abandonStart(ctx, created, cursorAgent, ctx.Err()); err != context.Canceled {
		t.Fatalf("abandonStart: %v", err)
	}
	assertTrace(t, f.trace("pane.send_keys"), []string{
		sendKeys("w1:p2", "ctrl+c"), sendKeys("w1:p2", "ctrl+c"),
	})
	closesNothing(t, f)
}

func TestAbandonStartWithoutReportedAgentSendsNoKeys(t *testing.T) {
	n, f, _, created := newWorkAgentTestTab(t)
	old := abandonStartWait
	abandonStartWait = 20 * time.Millisecond
	t.Cleanup(func() { abandonStartWait = old })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.abandonStart(ctx, created, cursorAgent, ctx.Err()); err != context.Canceled {
		t.Fatalf("abandonStart: %v", err)
	}
	if got := f.trace("pane.send_keys"); len(got) != 0 {
		t.Errorf("sent unverified keys: %v", got)
	}
	closesNothing(t, f)
}

func TestAbandonStartLeavesAdoptedPaneUntouched(t *testing.T) {
	n, f, home, created := newWorkAgentTestTab(t)
	running := agentOf(cursorAgent, "", proto.StatusIdle, "w1:p2", "w1:t2", "w1", home, cursorAgent)
	f.reply("session.snapshot", snapshot(nil, nil, []any{running}, nil))
	f.reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adoptedCtx := withAdoptedPane(ctx, created, []string{"-zsh"})
	if err := n.abandonStart(adoptedCtx, created, cursorAgent, ctx.Err()); err != context.Canceled {
		t.Fatalf("abandonStart: %v", err)
	}
	if got := f.trace("session.snapshot", "pane.send_keys"); len(got) != 0 {
		t.Errorf("touched the adopted pane: %v", got)
	}
	closesNothing(t, f)
}

func TestWorkAgentStartRefusesWhenPaneHostRefreshFails(t *testing.T) {
	n, f, home := herdrNode(t)
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.handle("session.snapshot", func(map[string]any) any {
		return herdrFail{Code: "unavailable", Message: "snapshot failed"}
	})
	f.handle("agent.list", func(map[string]any) any {
		return herdrFail{Code: "unavailable", Message: "snapshot failed"}
	})
	_, err := n.startWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), home, cursorAgent, cursorAgent, "", "", "", "", "w1", "")
	if err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Fatalf("start: %v", err)
	}
	if got := f.trace("pane.send_input", "pane.send_keys"); len(got) != 0 {
		t.Errorf("typed into a pane whose host could not be checked: %v", got)
	}
	closesNothing(t, f)
}
