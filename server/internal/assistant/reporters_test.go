package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// reportingWithTask is a reporting conversation lark1 (working) whose task's agent s1 on box is working.
func reportingWithTask(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	s, f := newQuietService(t)
	ctx := context.Background()
	f.known["lark1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		proto.Session{SessionID: "lark1", MachineID: "server", Assistant: true, Status: proto.StatusWorking})
	s.markReporter(ctx, "server", "lark1")
	if _, err := s.toolTaskPlan(ctx, "lark1", planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	agentTurn(s, f, proto.StatusWorking, "", 0)
	return s, f
}

// interrupted are the sessions session.interrupt was called for, as machine/session.
func (f *fakeHub) interrupted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.Method != "session.interrupt" {
			continue
		}
		var a struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(c.Params, &a)
		out = append(out, c.Device+"/"+a.SessionID)
	}
	return out
}

func (f *fakeHub) sentTo(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.sends {
		if a["session_id"] == id {
			n++
		}
	}
	return n
}

// The stop button in an app (the hub's session.user_stopped): the turn is stopped already, the task's agent is
// interrupted and the conversation is marked stopped by the app.
func TestUserStoppedStopsAReportingConversation(t *testing.T) {
	s, f := reportingWithTask(t)
	s.UserStopped(context.Background(), SessionRef{MachineID: "server", SessionID: "lark1"})
	if got := strings.Join(f.interrupted(), " "); got != "box/s1" {
		t.Fatalf("interrupted %q", got)
	}
	b := &s.reporters
	b.mu.Lock()
	r := b.m["lark1"]
	b.mu.Unlock()
	if r.StoppedAt == nil || r.StoppedBy != "app" {
		t.Fatalf("reporter = %+v", r)
	}

	// Any other conversation stopped in the app is left alone.
	s.UserStopped(context.Background(), SessionRef{MachineID: "server", SessionID: "conv1"})
	if got := strings.Join(f.interrupted(), " "); got != "box/s1" {
		t.Fatalf("interrupted %q", got)
	}
}

// Clearing the main conversation's context (#5095) closes its own process only: a Feishu request it reports on is not
// stopped, so the Agents working on the tasks it handed out go on (unlike the Stop button above).
func TestClearMainLeavesItsTasksRunning(t *testing.T) {
	s, f := reportingWithTask(t)
	ctx := context.Background()
	if _, err := s.updateSettings(ctx, func(v *Settings) error {
		v.MachineID, v.Main, v.MainMachineID = "server", "lark1", "server"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := serve(s, "POST", "/api/v1/assistant/main/clear", `{"expect":"lark1"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"archived":"lark1"`) {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}
	if got := f.interrupted(); len(got) != 0 {
		t.Fatalf("interrupted %q", got)
	}
	if s.reporterStopped(ctx, "lark1") {
		t.Fatal("the reporting conversation was stopped")
	}
	f.mu.Lock()
	closed := 0
	for _, c := range f.calls {
		if c.Method == "session.close" && strings.Contains(string(c.Params), `"lark1"`) {
			closed++
		}
	}
	f.mu.Unlock()
	if closed != 1 {
		t.Fatalf("session.close for lark1: %d", closed)
	}
}
