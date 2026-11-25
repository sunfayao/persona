package assistant

import (
	"context"
	"encoding/json"
	"github.com/nwverse/persona/server/internal/proto"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

const outcomeReply = "修好了：登录页在横屏时用了固定宽度。PR https://github.com/o/weather/pull/12 已合并。注意 iPad 模拟器要用原版的，瘦身的复现不了。"

// What the model says is kept within bounds: one line, links that are links, at most five of each.
func TestTidyOutcome(t *testing.T) {
	var o taskOutcome
	_ = json.Unmarshal([]byte(`{"outcome":"  一\n二  ","links":["https://a/1","https://a/1","ftp://x","https://a/2","https://a/3","https://a/4"],
		"pitfalls":["a","a","","b","c","d","e","f"]}`), &o)
	got := tidyOutcome(o)
	if got.Outcome != "一 二" || len(got.Links) != 3 || got.Links[1] != "https://a/2" || len(got.Pitfalls) != 5 || got.Pitfalls[1] != "b" {
		t.Fatalf("tidy = %+v", got)
	}
}

type phaseOutcomeStore struct {
	*ctxAwareLinkStore
	decisions []db.TodoDecisionRow
}

func (s *phaseOutcomeStore) ListTodoDecisions(context.Context, string, bool) ([]db.TodoDecisionRow, error) {
	return append([]db.TodoDecisionRow(nil), s.decisions...), nil
}

func TestTodoOutcomeAfterReopening(t *testing.T) {
	ctx := context.Background()
	store := &phaseOutcomeStore{ctxAwareLinkStore: newCtxAwareLinkStore()}
	s := newServiceOn(t, newFakeHub(), store)
	first := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	todo := db.TodoRow{ID: "td_phases", Title: "Publish the report", Notes: "Keep the original brief", CreatedAt: first.Add(-time.Hour)}
	store.rows = []db.TodoRow{todo}
	store.decisions = []db.TodoDecisionRow{{
		ID: "dec_first", TodoID: todo.ID, Question: "Outcome", Choice: "Ready to begin",
		Source: db.DecisionOutcome, SourceMachine: "box", SourceSession: "read-only",
		SourceMessage: outcomeKey(todo.ID, first) + "#o", CreatedAt: second.Add(time.Minute),
	}}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		t.Fatal(err)
	}
	setDone := func(at *time.Time) {
		s.todos.mu.Lock()
		s.todos.todos[todo.ID].DoneAt = at
		s.todos.mu.Unlock()
	}
	read := func() (string, []any) {
		t.Helper()
		output, err := s.toolTodoGet(ctx, "", "", json.RawMessage(`{"id":"td_phases"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output, todo.Title) || !strings.Contains(output, todo.Notes) {
			t.Fatalf("to-do brief lost: %s", output)
		}
		status, body := serveJSON(t, s, "GET", "/api/v1/todos/td_phases/decisions", "en", "")
		if status != 200 {
			t.Fatalf("decisions: %d, %v", status, body)
		}
		return output, body["decisions"].([]any)
	}
	setDone(&first)
	_, rows := read()
	if d := rows[0].(map[string]any); d["question"] != "Outcome" {
		t.Fatalf("first completion: %v", d)
	}

	setDone(nil)
	out, rows := read()
	if !strings.Contains(out, "Earlier phase outcome → Ready to begin") || !strings.Contains(out, "read-only") {
		t.Fatalf("reopened tool result: %s", out)
	}
	if d := rows[0].(map[string]any); d["question"] != "Earlier phase outcome" ||
		d["phase_completed_at"] != first.Format(time.RFC3339) || d["source_session"] != "read-only" || d["source_message"] != store.decisions[0].SourceMessage {
		t.Fatalf("reopened decisions: %v", d)
	}

	setDone(&second)
	store.decisions = append(store.decisions, db.TodoDecisionRow{
		ID: "dec_second", TodoID: todo.ID, Question: "Outcome", Choice: "Report published",
		Source: db.DecisionOutcome, SourceMachine: "box", SourceSession: "publish",
		SourceMessage: outcomeKey(todo.ID, second) + "#o", CreatedAt: second,
	})
	out, rows = read()
	if !strings.Contains(out, "Earlier phase outcome → Ready to begin") ||
		!strings.Contains(out, "Outcome → Report published") || strings.Contains(out, "Earlier phase outcome → Report published") {
		t.Fatalf("completed tool result: %s", out)
	}
	if rows[0].(map[string]any)["question"] != "Earlier phase outcome" || rows[1].(map[string]any)["question"] != "Outcome" {
		t.Fatalf("completed decisions: %v", rows)
	}
}
