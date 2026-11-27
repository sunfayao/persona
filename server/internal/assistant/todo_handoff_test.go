package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

const handoffCheck = "Verify the release candidate"

func handoffTestTodos(t *testing.T, store Store) (*Service, *fakeHub, Todo, Todo, Todo) {
	t.Helper()
	s, f := newQuietServiceOn(t, store)
	presenceTimes(t, 0, 0)
	ownerTurnTimes(t, 0, 0)
	ctx := context.Background()
	var runs []proto.Session
	add := func(title, sid string) Todo {
		task, err := s.createTodo(ctx, title, "", "conv1", "")
		if err != nil {
			t.Fatal(err)
		}
		s.superviseTodo(ctx, task.ID, "conv1", []string{handoffCheck}, 120)
		if err = s.linkTodoAgent(ctx, task.ID, "box", sid, title, "/code/app", "codex"); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, proto.Session{MachineID: "box", SessionID: sid, Status: proto.StatusIdle, State: proto.StateIdle,
			Agent: "codex", UpdatedAt: time.Now()})
		return todoNow(s, task.ID)
	}
	a, b, receiving := add("Candidate A", "origin-a"), add("Candidate B", "origin-b"), add("Release verification", "receiver")
	f.setSessions("box", runs...)
	f.mu.Lock()
	f.nodes = map[string]nodeAnswer{
		DeviceSessionSend: func(_ string, raw json.RawMessage) (any, error) {
			var params map[string]any
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, err
			}
			f.agentSent = append(f.agentSent, params)
			return map[string]bool{"ok": true, "queued": true}, nil
		},
	}
	f.mu.Unlock()
	return s, f, a, b, receiving
}

func sendHandoffTest(t *testing.T, s *Service, f *fakeHub, self, origin, receiving, head string) (map[string]string, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{
		"machine": "box", "session_id": "receiver", "text": "Please run the release check.",
		"handoff": map[string]string{
			"todo_id": origin, "receiving_todo_id": receiving, "head": head, "check": handoffCheck, "until": "2h",
		},
	})
	_, err := s.runTool(context.Background(), "server", self, "agent_send", args)
	if err != nil {
		return nil, err
	}
	var receipt map[string]string
	for _, line := range strings.Split(strings.Join(sentToAgent(f), "\n"), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var fields map[string]string
		if json.Unmarshal([]byte(line), &fields) == nil && fields["todo_id"] == origin {
			// The latest request for this origin is the one its Agent just received.
			receipt = fields
		}
	}
	return receipt, nil
}

func reportHandoffTest(s *Service, session, kind string, receipt map[string]string) (string, error) {
	args, _ := json.Marshal(map[string]any{"kind": kind, "text": "The release check ran.",
		"evidence": []string{"check-release: 4 passed"}, "handoff_receipt": receipt})
	return s.reportToSupervisor(context.Background(), "box", session, args, time.Now())
}

func TestTaskHandoffRequiresDeliveryConfirmation(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"no acknowledgement", nil},
		{"unknown delivery", context.DeadlineExceeded},
		{"node refused", errors.New("Agent cannot take the message")},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, a, _, receiving := handoffTestTodos(t, nil)
			f.mu.Lock()
			f.nodes[DeviceSessionSend] = func(_ string, _ json.RawMessage) (any, error) {
				return map[string]bool{"queued": true}, c.err
			}
			f.mu.Unlock()
			if _, err := sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, strings.Repeat("a", 40)); err == nil {
				t.Fatal("unconfirmed send should fail")
			}
			got := todoNow(s, a.ID).Supervision
			if got.State != a.Supervision.State || !got.UpdatedAt.Equal(a.Supervision.UpdatedAt) {
				t.Fatalf("unconfirmed send changed origin: %+v", got)
			}
		})
	}
}

func TestTaskHandoffEscalatesMissedResults(t *testing.T) {
	for _, tc := range []struct{ action, message string }{
		{"expiry", "Handoff deadline passed without a matching result"},
		{"failure", "Handoff failed"},
		{"cancel", "Receiving to-do cancelled or removed"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			s, f, a, _, receiving := handoffTestTodos(t, nil)
			r, err := sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, strings.Repeat("a", 40))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			switch tc.action {
			case "failure":
				if _, err = reportHandoffTest(s, "receiver", "blocked", r); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if _, err = reviewErr(s, receiving.ID, map[string]any{"verdict": "cancelled", "note": "Release cancelled"}); err != nil {
					t.Fatal(err)
				}
				s.superviseWatch(ctx, time.Now())
			case "expiry":
				s.superviseWatch(ctx, time.Now().Add(3*time.Hour))
			}
			settleService(t, s)
			if cur := todoNow(s, a.ID); cur.Supervision.State != reviewPending || cur.DoneAt != nil {
				t.Fatalf("expected an open to-do returned to review: %+v", cur.Supervision)
			}
			if !strings.Contains(strings.Join(f.notices(), "\n"), tc.message) {
				t.Fatalf("expected %q in supervision: %v", tc.message, f.notices())
			}
		})
	}
}

func TestTaskHandoffRespectsNewWorkAndOwnerDecision(t *testing.T) {
	s, f, a, _, receiving := handoffTestTodos(t, nil)
	head := strings.Repeat("a", 40)
	if _, err := sendHandoffTest(t, s, f, "side-chat", a.ID, receiving.ID, head); err == nil || len(sentToAgent(f)) != 0 {
		t.Fatal("an unrelated conversation may not send a handoff for the originating to-do")
	}
	f.mu.Lock()
	f.beforeSend = func() { s.taskWorking(context.Background(), "box", "origin-a") }
	f.mu.Unlock()
	if _, err := sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, head); err == nil || todoNow(s, a.ID).Supervision.State == reviewParked {
		t.Fatal("a node acknowledgement parked a work round that had already changed")
	}
	first, err := sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, head)
	if err != nil {
		t.Fatal(err)
	}
	s.taskWorking(context.Background(), "box", "origin-a")
	second, err := sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reportHandoffTest(s, "receiver", "done", first); err == nil {
		t.Fatal("the prior work round's receipt was accepted")
	}
	_, _, err = s.updateSupervision(context.Background(), a.ID, func(v *Supervision) bool {
		v.State = reviewNeedsUser
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reportHandoffTest(s, "receiver", "done", second); err == nil {
		t.Fatal("a receipt took over an owner decision")
	}
	if _, err = sendHandoffTest(t, s, f, "conv1", a.ID, receiving.ID, head); err == nil {
		t.Fatal("a new wait took over an owner decision")
	}
}
