package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The polling context can expire just after the last pre-budget list. A fresh final lookup after the budget is what
// closes that boundary race; the exact session becomes visible only to a list made after the deadline (#7772).
func TestTaskStartAdoptionLooksAgainAfterNodeStartBudget(t *testing.T) {
	s, f := newTaskService(t)
	shortAgentStartTimeout(t)
	oldWait, oldPoll := taskStartAdoptWait, taskStartAdoptPoll
	taskStartAdoptWait, taskStartAdoptPoll = 20*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { taskStartAdoptWait, taskStartAdoptPoll = oldWait, oldPoll })

	until := time.Now().Add(100 * time.Millisecond)
	postBudgetLists := 0
	f.mu.Lock()
	f.onList = func(f *fakeHub, machine string) {
		if machine != "box" || time.Now().Before(until) {
			return
		}
		postBudgetLists++
		f.sessions["box"] = []proto.Session{{SessionID: "exact-after-budget", MachineID: "box"}}
	}
	f.mu.Unlock()

	got, ok := s.adoptTaskStartSessionUntil(context.Background(), Device{ID: "box"}, "claude", "/code/app", "exact-after-budget", nil, until)
	if !ok || got.SessionID != "exact-after-budget" {
		t.Fatalf("got %+v ok=%v, want a fresh exact-id lookup after the node start budget", got, ok)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if postBudgetLists != 1 {
		t.Fatalf("post-budget lists = %d, want exactly one", postBudgetLists)
	}
}
