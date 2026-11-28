package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodoDecisions(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"todo_a", "todo_b"} {
		if err := d.CreateTodo(ctx, TodoRow{ID: id, Title: id, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	add := func(r TodoDecisionRow) bool {
		t.Helper()
		ok, err := d.InsertTodoDecision(ctx, r)
		if err != nil {
			t.Fatalf("insert %s: %v", r.ID, err)
		}
		return ok
	}
	add(TodoDecisionRow{ID: "d1", TodoID: "todo_a", Question: "用哪个库？", Choice: "pgx", Reason: "已经在用", Source: DecisionManual, CreatedAt: base.Add(time.Minute)})
	add(TodoDecisionRow{ID: "d2", TodoID: "todo_a", Question: "迁移号", Choice: "0311", Source: DecisionCard,
		SourceMachine: "box", SourceSession: "s1", SourceMessage: "ix1", CreatedAt: base.Add(2 * time.Minute)})
	// The same card's answer again is not a second decision.
	if add(TodoDecisionRow{ID: "d2b", TodoID: "todo_a", Question: "迁移号", Choice: "0311", Source: DecisionCard,
		SourceMachine: "box", SourceSession: "s1", SourceMessage: "ix1", CreatedAt: base.Add(3 * time.Minute)}) {
		t.Fatal("a card's answer was written twice")
	}
	// Manual decisions with the same question are separate decisions.
	add(TodoDecisionRow{ID: "d3", TodoID: "todo_a", Question: "用哪个库？", Choice: "database/sql", Reason: "少一个依赖",
		Source: DecisionManual, SupersedesID: "d1", CreatedAt: base.Add(4 * time.Minute)})

	live, err := d.ListTodoDecisions(ctx, "todo_a", false)
	if err != nil {
		t.Fatal(err)
	}
	if ids(live) != "d2,d3" {
		t.Fatalf("in effect = %s, want d2,d3", ids(live))
	}
	if live[1].SupersedesID != "d1" || live[0].SourceMessage != "ix1" || live[0].SourceMachine != "box" {
		t.Fatalf("rows = %+v", live)
	}
	all, err := d.ListTodoDecisions(ctx, "todo_a", true)
	if err != nil {
		t.Fatal(err)
	}
	if ids(all) != "d1,d2,d3" || all[0].SupersededBy != "d3" || all[2].SupersededBy != "" {
		t.Fatalf("all = %+v", all)
	}
	if got, _ := d.GetTodo(ctx, "todo_a"); !got.UpdatedAt.Equal(base.Add(4 * time.Minute)) {
		t.Fatalf("todo updated_at = %v", got.UpdatedAt)
	}

	// Only an earlier decision of the same todo can be superseded.
	add(TodoDecisionRow{ID: "e1", TodoID: "todo_b", Question: "q", Choice: "c", Source: DecisionManual, CreatedAt: base.Add(time.Minute)})
	for name, r := range map[string]TodoDecisionRow{
		"other todo's": {ID: "x1", TodoID: "todo_a", Question: "q", Choice: "c", SupersedesID: "e1", CreatedAt: base.Add(time.Hour)},
		"missing":      {ID: "x2", TodoID: "todo_a", Question: "q", Choice: "c", SupersedesID: "nope", CreatedAt: base.Add(time.Hour)},
		"later":        {ID: "x3", TodoID: "todo_a", Question: "q", Choice: "c", SupersedesID: "d3", CreatedAt: base},
	} {
		if _, err := d.InsertTodoDecision(ctx, r); !errors.Is(err, ErrDecisionSupersede) {
			t.Fatalf("%s: err = %v, want ErrDecisionSupersede", name, err)
		}
	}
	if _, err := d.InsertTodoDecision(ctx, TodoDecisionRow{ID: "x4", TodoID: "gone", Question: "q", Choice: "c", CreatedAt: base}); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("missing todo: err = %v", err)
	}

	// Marking afterwards: d2 superseded by d3 too; then d3 no longer supersedes anything and d1 is back in effect.
	if _, err := d.SetTodoDecisionSupersedes(ctx, "todo_a", "d2", "d3", base.Add(time.Hour)); !errors.Is(err, ErrDecisionSupersede) {
		t.Fatalf("superseding a later one: err = %v", err)
	}
	if _, err := d.SetTodoDecisionSupersedes(ctx, "todo_a", "d2", "d2", base.Add(time.Hour)); !errors.Is(err, ErrDecisionSupersede) {
		t.Fatalf("superseding itself: err = %v", err)
	}
	if _, err := d.SetTodoDecisionSupersedes(ctx, "todo_b", "d2", "", base.Add(time.Hour)); !errors.Is(err, ErrDecisionNotFound) {
		t.Fatalf("another todo's decision: err = %v", err)
	}
	got, err := d.SetTodoDecisionSupersedes(ctx, "todo_a", "d3", "d2", base.Add(time.Hour))
	if err != nil || got.SupersedesID != "d2" {
		t.Fatalf("set = %+v, %v", got, err)
	}
	live, _ = d.ListTodoDecisions(ctx, "todo_a", false)
	if ids(live) != "d1,d3" {
		t.Fatalf("after re-marking, in effect = %s, want d1,d3", ids(live))
	}
	if _, err := d.SetTodoDecisionSupersedes(ctx, "todo_a", "d3", "", base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	live, _ = d.ListTodoDecisions(ctx, "todo_a", false)
	if ids(live) != "d1,d2,d3" {
		t.Fatalf("after clearing, in effect = %s", ids(live))
	}

	// Deleting the todo takes its decisions.
	if err := d.DeleteTodo(ctx, "todo_a"); err != nil {
		t.Fatal(err)
	}
	if left, _ := d.ListTodoDecisions(ctx, "todo_a", true); len(left) != 0 {
		t.Fatalf("left after delete: %+v", left)
	}
}

func ids(rows []TodoDecisionRow) string {
	s := ""
	for i, r := range rows {
		if i > 0 {
			s += ","
		}
		s += r.ID
	}
	return s
}

// TodoDecisionsSince is every todo's decisions in effect written inside the window, oldest first.
func TestTodoDecisionsSince(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"todo_a", "todo_b"} {
		if err := d.CreateTodo(ctx, TodoRow{ID: id, Title: id, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []TodoDecisionRow{
		{ID: "old", TodoID: "todo_a", Question: "Merge the PR now?", Choice: "Yes", Source: DecisionCard, SourceMessage: "ix0", CreatedAt: base.Add(-time.Minute)},
		{ID: "d1", TodoID: "todo_a", Question: "What next?", Choice: "Wait", Source: DecisionManual, CreatedAt: base},
		{ID: "d2", TodoID: "todo_b", Question: "Merge the PR now?", Choice: "Yes", Source: DecisionCard,
			SourceMachine: "box", SourceSession: "s1", SourceMessage: "ix1", CreatedAt: base.Add(time.Minute)},
		{ID: "d3", TodoID: "todo_a", Question: "What next?", Choice: "Merge", Source: DecisionManual, SupersedesID: "d1", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "d4", TodoID: "todo_b", Question: "Push?", Choice: "Yes", Source: DecisionAgent, CreatedAt: base.Add(3 * time.Minute)},
	} {
		if ok, err := d.InsertTodoDecision(ctx, r); err != nil || !ok {
			t.Fatalf("insert %s: %v %v", r.ID, ok, err)
		}
	}
	rows, err := d.TodoDecisionsSince(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	// Not the one from before the window, nor the one a later decision replaced; both todos', in the order written.
	if ids(rows) != "d2,d3,d4" {
		t.Fatalf("since = %s, want d2,d3,d4", ids(rows))
	}
	if r := rows[0]; r.TodoID != "todo_b" || r.Source != DecisionCard || r.SourceMessage != "ix1" || r.SourceMachine != "box" ||
		r.Choice != "Yes" || !r.CreatedAt.Equal(base.Add(time.Minute)) || rows[1].SupersedesID != "d1" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows, err := d.TodoDecisionsSince(ctx, base.Add(time.Hour)); err != nil || len(rows) != 0 {
		t.Fatalf("after the last: %+v, %v", rows, err)
	}
}

func TestTodoOutcomeSecondWriteRollsBackAndRetries(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	const todoID, key = "todo_outcome", "outcome:todo_outcome:completion"
	if err := d.CreateTodo(ctx, TodoRow{ID: todoID, Title: "Ship", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	rows := []TodoDecisionRow{
		{ID: "summary", TodoID: todoID, Source: DecisionOutcome, SourceMessage: key + "#o", Question: "Outcome", Choice: "Done", CreatedAt: at},
		{ID: "summary", TodoID: todoID, Source: DecisionOutcome, SourceMessage: key + "#d1", Question: "How", Choice: "Batch", CreatedAt: at.Add(time.Microsecond)},
		{ID: "pitfall", TodoID: todoID, Source: DecisionOutcome, SourceMessage: key + "#p1", Question: "Pitfall", Choice: "Retry duplicate", CreatedAt: at.Add(2 * time.Microsecond)},
	}
	if got, err := d.CommitTodoOutcome(ctx, todoID, key, rows); err == nil || len(got) != 0 {
		t.Fatalf("second row's duplicate ID did not abort the batch: %v, %v", got, err)
	}
	if got, err := d.ListTodoDecisions(ctx, todoID, true); err != nil || len(got) != 0 {
		t.Fatalf("partial batch visible: %v, %v", got, err)
	}
	if recorded, err := d.TodoOutcomeRecorded(ctx, todoID, key); err != nil || recorded {
		t.Fatalf("partial batch marked complete: %v, %v", recorded, err)
	}
	rows[1].ID = "decision"
	start := make(chan struct{})
	type result struct {
		count int
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			got, err := d.CommitTodoOutcome(ctx, todoID, key, rows)
			results <- result{len(got), err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || !((a.count == 3 && b.count == 0) || (a.count == 0 && b.count == 3)) {
		t.Fatalf("the two retries must commit one batch: %+v, %+v", a, b)
	}
	if got, err := d.ListTodoDecisions(ctx, todoID, true); err != nil || ids(got) != "summary,decision,pitfall" {
		t.Fatalf("retried batch: %v, %v", got, err)
	}
	if recorded, err := d.TodoOutcomeRecorded(ctx, todoID, key); err != nil || !recorded {
		t.Fatalf("committed batch marker: %v, %v", recorded, err)
	}
}

func TestTodoOutcomeRetryFillsMissingLegacySlots(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	const todoID, key = "todo_legacy", "outcome:todo_legacy:completion"
	if err := d.CreateTodo(ctx, TodoRow{ID: todoID, Title: "Ship", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	summary := TodoDecisionRow{ID: "legacy", TodoID: todoID, Source: DecisionOutcome, SourceMessage: key + "#o",
		Question: "Outcome", Choice: "Original summary", CreatedAt: at}
	if ok, err := d.InsertTodoDecision(ctx, summary); err != nil || !ok {
		t.Fatalf("legacy summary: %v, %v", ok, err)
	}
	newSummary := summary
	newSummary.ID, newSummary.Question = "new", "Phase outcome"
	decision := TodoDecisionRow{ID: "recovered", TodoID: todoID, Source: DecisionOutcome, SourceMessage: key + "#d1",
		Question: "What worked?", Choice: "Use the existing pipeline", CreatedAt: at.Add(time.Microsecond)}
	if got, err := d.CommitTodoOutcome(ctx, todoID, key, []TodoDecisionRow{newSummary, decision}); err != nil || ids(got) != "recovered" {
		t.Fatalf("retry: %v, %v", got, err)
	}
	if got, err := d.ListTodoDecisions(ctx, todoID, true); err != nil || ids(got) != "legacy,recovered" || got[0].Choice != summary.Choice {
		t.Fatalf("legacy decisions: %v, %v", got, err)
	}
}
