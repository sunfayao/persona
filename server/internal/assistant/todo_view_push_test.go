package assistant

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The To-dos view stays fresh (todo_view_push.go, #10226): the apps are told to read the list again
// ({"type":"todos"}) when what a row is filed under changes with no write to its to-do, because one of its runs
// changed or time alone moved it; never for a working run's every step.

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
