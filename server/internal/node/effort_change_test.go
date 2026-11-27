package node

import (
	"context"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestQueuedEffortFailureRetainsTheSendBarrier(t *testing.T) {
	n, f, _, cwd, _ := switchNode(t, proto.StatusBlocked, nil)
	n.noteModes(sidA, "", "low")
	if err := pick(t, n, map[string]any{"effort": "high", "queue_effort": true}); err != nil {
		t.Fatal(err)
	}
	f.setScreens("w1:p1", screenFixture(t, "claude-2.1.312-typed-162.txt"), screenFixture(t, "claude-2.1.312-typed-162.ansi"))
	f.reply("session.snapshot", snapshot(nil, nil, []any{claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, "")}, nil))
	waitFor(t, "the draft refusal on the row", func() bool {
		row := rowOf(t, n, sidA)
		return row.Effort == "low" && row.PendingEffort != nil && *row.PendingEffort == "high" && row.EffortChangeFailed
	})
	if code, _ := refusalOf(t, sendWaitingForEffort(t, n)); code != proto.CodeEffortChangePending {
		t.Fatalf("send refusal = %q", code)
	}
	if err := pick(t, n, map[string]any{"effort": "low", "queue_effort": true}); err != nil {
		t.Fatal(err)
	}
	if row := rowOf(t, n, sidA); row.PendingEffort != nil || row.EffortChangeFailed || row.Effort != "low" {
		t.Fatalf("the current effort did not clear the pending change: %+v", row)
	}
	if got := drove(f); len(got) != 0 {
		t.Fatalf("touched the draft: %q", got)
	}
}

func TestQueuedEffortRechecksIdleBeforeTyping(t *testing.T) {
	n, f, _, _, _ := switchNode(t, proto.StatusWorking, nil)
	if err := pick(t, n, map[string]any{"effort": "high", "queue_effort": true}); err != nil {
		t.Fatal(err)
	}
	// Simulate a list built from an idle snapshot just before another turn started.
	n.wakeEffortChange(&proto.Session{SessionID: sidA, PaneID: "w1:p1", Status: proto.StatusIdle, PendingEffort: new("high")})
	waitCalls(t, f, "session.snapshot", 2)
	unlock, err := n.inputTurn(context.Background(), sidA)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if change := n.switches().effortChange(sidA); change.target != "high" || change.failed {
		t.Fatalf("the effort request did not stay pending: %+v", change)
	}
	if got := drove(f); len(got) != 0 {
		t.Fatalf("drove Claude during a new turn: %q", got)
	}
}

func sendWaitingForEffort(t *testing.T, n *Node) error {
	t.Helper()
	_, err := request(t, n, "session.send", map[string]any{
		"session_id": sidA, "text": "follow-up", "wait_for_effort": true,
	})
	return err
}

func TestFailedQueuedEffortClearsWhenPaneCloses(t *testing.T) {
	n, f, _, _, _ := switchNode(t, proto.StatusBlocked, nil)
	n.switches().setEffortChange(sidA, effortChange{target: "high", pane: "w1:p1", failed: true})
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	n.tryEffortChange(sidA)
	if change := n.switches().effortChange(sidA); change.target != "" || change.failed {
		t.Fatalf("closed pane left a failed effort change: %+v", change)
	}
}
