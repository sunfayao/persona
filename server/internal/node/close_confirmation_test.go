package node

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The cached allow or lost-first reason is not enough: if the dialog is gone from the screen now, the
// forced close is still refused.
func TestForceCloseStaleAllowNeedsCurrentDialog(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, []any{claudeAgent(sidA, proto.StatusWorking, "w1:p1", "w1:t1", "w1", "/tmp", "")}, nil))
	f.setScreen("w1:p1", screenFixture(t, "claude-2.1.312-empty-162.txt"))
	listSessions(t, n)
	n.allowed.m = map[string]*allowWatch{sidA: {head: "Permissions"}}
	f.reset()
	_, err := request(t, n, "session.close", map[string]any{"session_id": sidA, "force": true})
	if code, _ := refusalOf(t, err); code != "agent_busy" {
		t.Fatalf("stale allow close: %v", err)
	}
	if got := strings.Join(f.trace(), "\n"); strings.Contains(got, "send_keys") || strings.Contains(got, "tab.close") {
		t.Fatalf("stale allow touched the terminal: %s", got)
	}
}

// A resting Claude with no dialog on its screen is not waiting for confirmation: still refused.
func TestForceCloseIdleWithoutDialog(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, []any{claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp", "")}, nil))
	f.setScreen("w1:p1", screenFixture(t, "claude-2.1.312-empty-162.txt"))
	listSessions(t, n)
	f.reset()
	_, err := request(t, n, "session.close", map[string]any{"session_id": sidA, "force": true})
	if code, _ := refusalOf(t, err); code != "agent_busy" {
		t.Fatalf("idle close without a dialog: %v", err)
	}
	if got := strings.Join(f.trace(), "\n"); strings.Contains(got, "tab.close") {
		t.Fatalf("idle Agent closed: %s", got)
	}
}

func TestForceCloseRuntimeConfirmation(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := dshHello()
	hello["caps"].(map[string]any)["close"] = true
	host := dialRuntime(t, n, hello)
	for _, status := range []string{proto.StatusBlocked, proto.StatusWorking} {
		_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "confirm-runtime", "status": status}}})
		waitFor(t, "current runtime status", func() bool { return listed(t, n)["confirm-runtime"].Status == status })
		_, err := nodeCall(t, n, "session.close", map[string]any{"session_id": "confirm-runtime", "force": true})
		if status == proto.StatusBlocked && err != nil {
			t.Fatal(err)
		}
		if status == proto.StatusWorking && err == nil {
			t.Fatal("working runtime was force-closed")
		}
	}
	calls := host.got("session.close")
	if len(calls) != 1 {
		t.Fatalf("runtime closes: %s", calls)
	}
	var params struct {
		Force bool `json:"force"`
	}
	_ = json.Unmarshal(calls[0], &params)
	if !params.Force {
		t.Fatal("runtime did not get guarded close")
	}
}
