package assistant

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestLeaderToolWatchDoesNotCacheStateReadFailure(t *testing.T) {
	ft := newFleetTest(t)
	s := ft.s
	ctx := context.Background()
	cursor, _ := s.mcpChanges.snapshot()
	s.mcpChanges.notify()
	ft.st.setFailSetting(errors.New("database down"))
	failed := s.sessionMCPToolsSince(ctx, "mac", "leader", false, false, cursor)
	if failed["error"] == nil || failed["revision"] != nil || failed["tools"] != nil {
		t.Fatalf("failed state read became a cacheable empty list: %+v", failed)
	}
	ft.st.setFailSetting(nil)
	// No further task-link change: retrying the unconsumed cursor must recover.
	recovered := s.sessionMCPToolsSince(ctx, "mac", "leader", false, false, cursor)
	if recovered["error"] != nil || recovered["revision"] == nil || recovered["unchanged"] == true {
		t.Fatalf("state recovery needs another link change: %+v", recovered)
	}
}

func TestLeaderToolWatchCancellationAndRestart(t *testing.T) {
	s, _ := newQuietService(t)
	cursor, changed := s.mcpChanges.snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := s.sessionMCPToolsSince(ctx, "box", "unknown", false, false, cursor)
	if got["unchanged"] != true || got["revision"] != cursor {
		t.Fatalf("cancelled watch: %+v", got)
	}
	select {
	case <-changed:
		t.Fatal("cancellation changed rights")
	default:
	}
	other, _ := newQuietService(t)
	restarted := other.sessionMCPToolsSince(context.Background(), "box", "unknown", false, false, cursor)
	if restarted["revision"] == cursor || restarted["unchanged"] == true {
		t.Fatalf("restart did not invalidate the old cursor: %+v", restarted)
	}
}

func TestLeaderWatchRegisteredBeforeLink(t *testing.T) {
	s, _ := newQuietService(t)
	ctx := context.Background()
	todo, err := s.createTaskTodo(ctx, "Leader", "", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := s.mcpChanges.snapshot()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan map[string]any, 1)
	go func() { result <- s.sessionMCPToolsSince(watchCtx, "box", "leader", false, false, cursor) }()
	if err := s.linkPlanAgent(ctx, PlanTask{TodoID: todo.ID, Role: roleLeader}, "box", "leader", false); err != nil {
		t.Fatal(err)
	}
	got := <-result
	if !slices.Contains(leaderToolNames(got["tools"].([]proto.Tool)), "task_review") {
		t.Fatalf("watch result: %+v", got)
	}
}
