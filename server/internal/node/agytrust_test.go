package node

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// agyTrustScreen is agy's folder-trust dialog as the Antigravity CLI draws it in a folder it has not trusted (#11424).
const agyTrustScreen = "Accessing workspace:\n\n/work/example-project\n\nDo you trust the contents of this project?\n\n" +
	"Antigravity CLI requires permission to read, edit, and execute files here.\n\n> Yes, I trust this folder\n  No, exit\n\n" +
	"  ↑/↓ Navigate · enter Confirm\n                                                   auto · Example model · medium\n"

// agyTrustKeys picks "Yes, I trust this folder" wherever the cursor is, and answers no other dialog.
func TestAgyTrustKeys(t *testing.T) {
	moved := strings.Replace(strings.Replace(agyTrustScreen, "> Yes", "  Yes", 1), "  No, exit", "> No, exit", 1)
	other := strings.Replace(agyTrustScreen, "Do you trust the contents of this project?", "Run this command?", 1)
	for name, tc := range map[string]struct {
		screen string
		want   []string
	}{
		"cursor on yes":  {agyTrustScreen, []string{"enter"}},
		"cursor on no":   {moved, []string{"up", "enter"}},
		"another dialog": {other, nil},
		"input box":      {agyIdleScreen, nil},
	} {
		if got := agyTrustKeys(tc.screen); !slices.Equal(got, tc.want) {
			t.Errorf("%s: keys %v, want %v", name, got, tc.want)
		}
	}
}

// A message typed into an agy or Cursor pane keeps its row working until herdr shows the turn, for workSentGrace.
func TestWorkAgentStatusAfterASend(t *testing.T) {
	n, _, _ := herdrNode(t)
	now := time.Now()
	idle := host.Agent{Agent: agyAgent, PaneID: "w1:p1", AgentStatus: proto.StatusIdle}
	if got := n.liveStatus("agy-w1-p1", idle, now); got != proto.StatusIdle {
		t.Fatalf("before a send: %q", got)
	}
	n.workSent.mark("w1:p1", now)
	if got := n.liveStatus("agy-w1-p1", idle, now.Add(time.Second)); got != proto.StatusWorking {
		t.Errorf("right after a send: %q, want working", got)
	}
	blocked := idle
	blocked.AgentStatus = proto.StatusBlocked
	if got := n.liveStatus("agy-w1-p1", blocked, now.Add(time.Second)); got != proto.StatusBlocked {
		t.Errorf("blocked right after a send: %q, want blocked", got)
	}
	if got := n.liveStatus("agy-w1-p1", idle, now.Add(workSentGrace+time.Second)); got != proto.StatusIdle {
		t.Errorf("after the grace: %q, want idle", got)
	}
	other := host.Agent{Agent: "claude", PaneID: "w1:p2", AgentStatus: proto.StatusIdle}
	n.workSent.mark("w1:p2", now)
	if got := n.workAgentStatus(other, now); got != proto.StatusWorking {
		t.Errorf("workAgentStatus itself: %q", got)
	}
}

// session.send to an agy pane marks it, so the row the hub lists reads working right after (#11437).
func TestAgySendReadsWorkingUntilHerdrShowsTheTurn(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, ""))
	f.reply("agent.prompt", map[string]any{})
	f.after(func(c herdrCall) {
		if c.Method == "agent.prompt" {
			f.setScreen(agyTestPane, "count to 300 (alt+m to send immediately)")
		}
	})
	if _, err := request(t, n, "session.send", map[string]any{"session_id": agyTestRow, "text": "count to 300"}); err != nil {
		t.Fatal(err)
	}
	a, ok := n.agent(agyTestRow)
	if !ok {
		t.Fatal("no agy row")
	}
	if got := n.liveStatus(agyTestRow, a, time.Now()); got != proto.StatusWorking {
		t.Errorf("right after the send: %q, want working", got)
	}
}
