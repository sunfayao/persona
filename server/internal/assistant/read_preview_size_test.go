package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/readsize"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A large fleet's default agents_list stays small enough for a preview, and keeps what a caller acts on: every id of
// the rows shown, model=, todo= and reuse=, the group that needs the user, and the way to the rows left out.
func TestAgentsListDefaultStaysSmallAndKeepsIdsAndHints(t *testing.T) {
	s, f := newQuietService(t)
	now := time.Now()
	words := strings.Repeat("a long conclusion the model does not need in full ", 20)
	var sessions []proto.Session
	for i := 0; i < 12; i++ {
		sessions = append(sessions, proto.Session{SessionID: fmt.Sprintf("work%04d", i), MachineID: "box", Cwd: fmt.Sprintf("/code/work%d", i),
			Agent: "claude", Title: "working", Status: proto.StatusIdle, State: proto.StateWorking, Model: "model-w", UpdatedAt: now.Add(-time.Duration(i) * time.Minute),
			LastText: words})
	}
	sessions = append(sessions, proto.Session{SessionID: "need0001", MachineID: "box", Cwd: "/code/need", Agent: "claude", Title: "needs you",
		Status: proto.StatusIdle, State: proto.StateNeedsYou, Model: "model-n", UpdatedAt: now, AttentionReason: words})
	for i := 0; i < 15; i++ {
		sessions = append(sessions, proto.Session{SessionID: fmt.Sprintf("idle%04d", i), MachineID: "box", Cwd: fmt.Sprintf("/code/idle%d", i),
			Agent: "claude", Title: "idle", Status: proto.StatusIdle, State: proto.StateIdle, Model: "model-i", UpdatedAt: now.Add(-time.Duration(i) * time.Hour / 10),
			Work: &proto.WorkState{Phase: proto.WorkDone, Line: words}, LastText: words})
	}
	for i := 0; i < 10; i++ {
		sessions = append(sessions, proto.Session{SessionID: fmt.Sprintf("endd%04d", i), MachineID: "box", Cwd: fmt.Sprintf("/code/endd%d", i),
			Agent: "claude", Title: "ended", Status: proto.StatusOffline, State: proto.StateOffline, Model: "model-e", UpdatedAt: now.Add(-time.Duration(i+1) * time.Minute),
			Work: &proto.WorkState{Phase: proto.WorkDone, Line: words}})
	}
	f.setSessions("box", sessions...)
	onBoxTask(t, s, "work0000", "", false)

	out, err := runJSON(t, s, "conv1", "agents_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > readsize.ListPreview {
		t.Fatalf("agents_list is %d bytes:\n%s", len(out), out)
	}
	for _, want := range []string{"session_id=need0001", "model=model-n", "session_id=work0000", "model=model-w", "todo=", "reuse=agent_send", "session_id=idle0000"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents_list lost %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "agents_list state=ended") && !strings.Contains(out, "agents_list state=idle") && !strings.Contains(out, "agents_list state=working") {
		t.Errorf("rows were left out without saying which state lists them:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "    conclusion:") || strings.HasPrefix(line, "    last said:") {
			if len(line) > readsize.FreeText+60 {
				t.Errorf("free text line is %d bytes: %s", len(line), line)
			}
		}
	}
	t.Logf("default agents_list for %d agents: %d bytes", len(sessions), len(out))
}

// agent_read without turns gives whole recent messages within the preview size, says what is left out and which call
// fetches it; a named turns keeps the old size; the supervisor's read goes the same way.
func TestAgentReadDefaultIsAPreview(t *testing.T) {
	s, f := newQuietService(t)
	f.setSessions("box", boxAgent("aaaa0001"))
	var items []proto.Item
	for i := 0; i < 12; i++ {
		items = append(items,
			proto.Item{ID: fmt.Sprintf("u%d", i), Kind: proto.KindText, Role: "user", Text: fmt.Sprintf("question %d %s", i, strings.Repeat("q", 1800))},
			proto.Item{ID: fmt.Sprintf("a%d", i), Kind: proto.KindText, Role: "assistant", Text: fmt.Sprintf("answer %d %s", i, strings.Repeat("a", 1800))})
	}
	f.answerNode(DeviceSessionItems, func(string, json.RawMessage) (any, error) { return map[string]any{"items": items}, nil })

	out, err := runJSON(t, s, "conv1", "agent_read", map[string]any{"machine": "box", "session_id": "aaaa0001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > readsize.TranscriptPreview+200 || !strings.HasPrefix(out, "[") || !strings.Contains(out, "agent_read with turns=2") ||
		!strings.Contains(out, "answer 11 ") || strings.Contains(out, "answer 0 ") || strings.Count(out, "…") > 0 {
		t.Fatalf("default read is %d bytes:\n%.400s", len(out), out)
	}
	if strings.Count(out, "Agent:") == 0 || strings.Contains(out, "\nUser: question 10 "+strings.Repeat("q", 100)) && !strings.Contains(out, strings.Repeat("q", 1800)) {
		t.Fatalf("a message was cut inside:\n%.400s", out)
	}
	big, err := runJSON(t, s, "conv1", "agent_read", map[string]any{"machine": "box", "session_id": "aaaa0001", "turns": 2})
	if err != nil || len(big) < 30000 || len(big) > 40100 {
		t.Fatalf("a named turns read: %d bytes, %v", len(big), err)
	}
	t.Logf("agent_read default %d bytes, turns=2 %d bytes", len(out), len(big))
}
