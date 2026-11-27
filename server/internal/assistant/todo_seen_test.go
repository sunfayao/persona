package assistant

import (
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Seen to-dos (todo_seen.go, #5707): Got it marks a to-do seen without counting as activity, Undo takes the mark off,
// the apps read it in the list and are told it changed; a seen to-do leaves Just finished.

func TestTodoSeenSurvivesRestart(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	id := e.create("发版").ID
	e.must("PUT", "/api/v1/todos/"+id+"/seen", "")
	again := &todoEnv{t: t, s: newServiceOn(t, e.f, database), f: e.f}
	if got := again.get(id); got == nil || got.SeenAt == nil {
		t.Fatalf("after a restart: %+v", got)
	}
}

// An open concern is never filed with the finished ones (#3582): seen, it waits in Other open for its report.
func TestTriageSeenConcernStaysOpen(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	moved, seen := now.Add(-time.Hour), now.Add(-30*time.Minute)
	sess := proto.Session{MachineID: "m", SessionID: "s", State: proto.StateUnread, UpdatedAt: moved}
	td := listedTodo{Todo: Todo{ID: "c", Title: "c", CreatedAt: moved, UpdatedAt: moved, Kind: db.KindConcern, SeenAt: &seen,
		Agents: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: moved}}}}
	if tr := triageTodo(td, lookupOf([]proto.Session{sess}), now, time.Time{}, time.UTC, false); tr.Section != triageOther || tr.Rule != ruleRest {
		t.Fatalf("a seen concern is %s / %s", tr.Section, tr.Rule)
	}
	td.Kind = ""
	if tr := triageTodo(td, lookupOf([]proto.Session{sess}), now, time.Time{}, time.UTC, false); tr.Section != triageDone || tr.Rule != ruleSeen {
		t.Fatalf("a seen to-do is %s / %s", tr.Section, tr.Rule)
	}
	r := triageTodos([]listedTodo{td}, lookupOf([]proto.Session{sess}), now, time.UTC, true)
	if r.Open != 0 || r.Finished != 1 {
		t.Fatalf("a seen finished turn counts %d open, %d finished", r.Open, r.Finished)
	}
}
