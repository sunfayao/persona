package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The To-dos view stays fresh (todo_view_push.go, #10226): the apps are told to read the list again
// ({"type":"todos"}) when what a row is filed under changes with no write to its to-do, because one of its runs
// changed or time alone moved it; never for a working run's every step.

// todosSent is how many {"type":"todos"} the apps were sent so far.
func todosSent(e *todoEnv) int {
	n := 0
	for _, ev := range strings.Fields(e.f.eventTypes()) {
		if ev == "todos" {
			n++
		}
	}
	return n
}

// todoViewsNow reads GET /api/v1/todos (which an app does after every push) and answers each row's view by id.
func todoViewsNow(e *todoEnv) map[string]assistantwire.TodoSummary {
	e.t.Helper()
	var got struct{ Todos []listedTodo }
	if err := json.Unmarshal(e.must("GET", "/api/v1/todos", "").Body.Bytes(), &got); err != nil {
		e.t.Fatal(err)
	}
	out := map[string]assistantwire.TodoSummary{}
	for _, r := range got.Todos {
		if r.View == nil {
			e.t.Fatalf("%s has no view", r.ID)
		}
		out[r.ID] = *r.View
	}
	return out
}

// asking is run c waiting on the owner since at, as the hub lists it.
func asking(c proto.Session, at time.Time) proto.Session {
	c.Status, c.State, c.PendingInteractions = proto.StatusBlocked, proto.StateNeedsYou, 1
	c.StateSince, c.UpdatedAt, c.Asks = at, at, "Trim the top too?"
	return c
}

// What a row is filed under follows its section, reason, asks, marks and steps, never its line, since,
// next_change_at or rank.
func TestTodoClassLeavesOutTheLine(t *testing.T) {
	at := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	working := func(text string) *assistantwire.TodoLine {
		return &assistantwire.TodoLine{Key: "working", Args: map[string]string{"text": text}}
	}
	base := assistantwire.TodoSummary{Phase: assistantwire.TodoPhaseWorking, Section: assistantwire.TodoListSectionWorking,
		Reason: assistantwire.TodoReasonAgentRunning, Line: working("a"), Since: at, NextChangeAt: at.Add(48 * time.Hour),
		Rank: 2}
	same := base
	same.Line, same.Since, same.NextChangeAt, same.Rank = working("b"), at.Add(time.Minute), at.Add(49*time.Hour), 0
	if todoClassOf(base) != todoClassOf(same) {
		t.Error("the line, since, next_change_at or rank changed what the row is filed under")
	}
	for name, change := range map[string]func(*assistantwire.TodoSummary){
		"section":  func(v *assistantwire.TodoSummary) { v.Section = assistantwire.TodoListSectionWaiting },
		"reason":   func(v *assistantwire.TodoSummary) { v.Reason = assistantwire.TodoReasonBackground },
		"asks":     func(v *assistantwire.TodoSummary) { v.Asks = []assistantwire.OwnerAsk{{ID: "run:server/s1@1"}} },
		"inactive": func(v *assistantwire.TodoSummary) { v.Inactive = at },
		"can_wait": func(v *assistantwire.TodoSummary) { v.CanWait = true },
		"seen":     func(v *assistantwire.TodoSummary) { v.Seen = true },
		"steps":    func(v *assistantwire.TodoSummary) { v.Steps = &assistantwire.TodoStepCount{Done: 1, Total: 2} },
	} {
		v := base
		change(&v)
		if todoClassOf(v) == todoClassOf(base) {
			t.Errorf("%s: the row is filed alike", name)
		}
	}
	// A run's ask whose card lands after it came to ask answers differently (its kind, its risk), with the same id.
	asking := base
	asking.Asks = []assistantwire.OwnerAsk{{ID: "run:server/s1@1", Kind: assistantwire.OwnerAskKindQuestion}}
	carded := asking
	carded.Asks = []assistantwire.OwnerAsk{{ID: "run:server/s1@1", Kind: assistantwire.OwnerAskKindTool, Interaction: "ix_1"}}
	if todoClassOf(carded) == todoClassOf(asking) {
		t.Error("a run's ask that got its card is filed alike")
	}
}

// workingRun is a run on its way since began, its latest line activity.
func workingRun(id string, began time.Time, activity string) proto.Session {
	return proto.Session{SessionID: id, Title: "Hedge trimming", Status: proto.StatusWorking, State: proto.StateWorking,
		StateSince: began, UpdatedAt: began, Activity: activity}
}
