package taskleader

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Every signal kind about a supervisor's own to-do has one route; the table is the contract: the main conversation
// hears of a supervised to-do only for its completion and for an alignment.
func TestEverySignalKindOfASupervisedTodoHasItsRoute(t *testing.T) {
	t.Parallel()
	want := map[string]Destination{
		KindReview:                ToSupervisor, // the completion review is the one exception below
		KindOwnerChoice:           ToMain,
		KindIdle:                  Absorbed,
		KindUnjudged:              Absorbed,
		KindNeedsYou:              ToSupervisor,
		KindTimedOut:              ToSupervisor,
		KindStalled:               Absorbed,
		KindNoProgress:            ToSupervisor,
		KindAllStopped:            ToSupervisor,
		KindInterrupted:           Absorbed,
		KindFailed:                ToMain,
		KindStarted:               Absorbed,
		KindPRConflict:            ToSupervisor,
		KindDraftPR:               ToSupervisor,
		KindPREvent:               ToSupervisor,
		KindReport:                Absorbed, // reports with a kind are below
		KindEscalated:             ToMain,
		KindOrphaned:              ToSupervisor,
		KindAsk:                   AsBefore,
		KindDecide:                ToMain,
		KindDecisionReply:         ToMain,
		KindCheck:                 ToSupervisor,
		KindTodoReady:             ToSupervisor,
		KindPRMerged:              ToSupervisor,
		KindParentCloseout:        ToSupervisor,
		KindRecheck:               ToMain,
		KindUndecidable:           ToMain,
		KindOwnerCannotAnswer:     ToMain,
		KindParkedDue:             ToSupervisor,
		KindParkDecided:           ToMain,
		KindParkedSummary:         Absorbed,
		KindNeedsUserExternalInfo: ToMain,
		KindInbox:                 AsBefore,
		KindDeliveryHold:          ToMain,
		KindCardlessWait:          ToMain,
		KindOverdue:               ToSupervisor,
		KindEstimateDue:           ToSupervisor,
		KindHandoffResult:         ToMain,
	}
	if len(Kinds) != 38 || len(want) != len(Kinds) {
		t.Fatalf("%d kinds listed, %d routed in this table", len(Kinds), len(want))
	}
	for _, kind := range Kinds {
		w, ok := want[kind]
		if !ok {
			t.Fatalf("kind %q has no row in this table", kind)
		}
		if got := Route(kind, ""); got != w {
			t.Errorf("%s goes to %s, want %s", kind, got, w)
		}
	}
	reports := []struct {
		kind, report string
		want         Destination
	}{
		{KindReview, ReportCompletion, ToMain},
		{KindReport, ReportProgress, Absorbed},
		{KindReport, ReportQuestion, ToMain},
		{KindReport, ReportBlocked, ToMain},
		{KindReport, ReportDone, Absorbed},
		{KindCheck, CheckDecided, ToMain},
	}
	for _, r := range reports {
		if got := Route(r.kind, r.report); got != r.want {
			t.Errorf("%s/%s goes to %s, want %s", r.kind, r.report, got, r.want)
		}
	}
	// None of the routine signals the main conversation used to get reaches it.
	for _, routine := range []string{KindEstimateDue, KindOverdue, KindIdle, KindStalled, KindNoProgress, KindOrphaned, KindStarted,
		KindParentCloseout, KindTodoReady, KindAllStopped, KindTimedOut, KindPRConflict, KindPRMerged} {
		if Route(routine, "") == ToMain {
			t.Errorf("routine signal %s reaches the main conversation", routine)
		}
	}
}

func TestTheSupervisorReportsDoneOnlyWithEveryWorkerClosedAndEveryCriterionEvidenced(t *testing.T) {
	t.Parallel()
	done := []Criterion{{Text: "login works", State: "met", Evidence: "go test ./... ok"}, {Text: "PR merged", State: "met", Evidence: "PR 7 merged"}}
	if why := CompletionRefusal(done, []WorkerState{{ID: "todo_a", Title: "a"}, {ID: "todo_b", Title: "b"}}); why != "" {
		t.Fatalf("a finished to-do was refused: %s", why)
	}
	why := CompletionRefusal(done, []WorkerState{{ID: "todo_a", Title: "a", Open: true}, {ID: "todo_b", Title: "b"}})
	if !strings.Contains(why, "1 worker(s) are still open") || !strings.Contains(why, "todo_a") || strings.Contains(why, "todo_b") {
		t.Fatalf("an open worker: %s", why)
	}
	half := []Criterion{{Text: "login works", State: "met", Evidence: "ok"}, {Text: "PR merged", State: "met"}, {Text: "docs", State: "pending"}}
	why = CompletionRefusal(half, nil)
	if !strings.Contains(why, `2 "PR merged"`) || !strings.Contains(why, `3 "docs"`) || strings.Contains(why, "login works") {
		t.Fatalf("criteria without evidence: %s", why)
	}
	if why := CompletionRefusal(nil, nil); why != "" {
		t.Fatalf("a to-do with no criteria and no workers left is refused: %s", why)
	}
}

func TestTheSupervisorsToolsTakeOnlyTheirOwnFields(t *testing.T) {
	t.Parallel()
	if a, err := DecodeRead(json.RawMessage(`{"todo_id":" todo_1 ","turns":3}`)); err != nil || a.TodoID != "todo_1" || a.Turns != 3 {
		t.Fatalf("read: %+v %v", a, err)
	}
	for _, bad := range []string{`{"machine":"box","session_id":"s1"}`, `{"todo_id":"t","conversation":"c"}`, `{"todo_id":""}`,
		`{"todo_id":"t","turns":11}`, `{"todo_id":"t"} {}`} {
		if _, err := DecodeRead(json.RawMessage(bad)); err == nil {
			t.Errorf("read took %s", bad)
		}
	}
	if a, err := DecodeSend(json.RawMessage(`{"todo_id":"todo_1","text":" continue "}`)); err != nil || a.Text != "continue" {
		t.Fatalf("send: %+v %v", a, err)
	}
	for _, bad := range []string{`{"todo_id":"t","text":"x","task":{"title":"new work"}}`, `{"todo_id":"t","text":""}`,
		`{"text":"x"}`, `{"todo_id":"t","text":"` + strings.Repeat("x", SendMax+1) + `"}`} {
		if _, err := DecodeSend(json.RawMessage(bad)); err == nil {
			t.Errorf("send took %.80s", bad)
		}
	}
	a, err := DecodeScope(json.RawMessage(`{"todo_id":"t","change":"more time","why":"tests are slow","expect_minutes":90}`))
	if err != nil || a.ExpectMinutes != 90 || a.DoneWhen != nil {
		t.Fatalf("scope: %+v %v", a, err)
	}
	for _, bad := range []string{`{"todo_id":"t","change":"x"}`, `{"todo_id":"t","change":"x","why":"y","expect_minutes":-1}`,
		`{"todo_id":"t","change":"x","why":"y","continues":"z"}`} {
		if _, err := DecodeScope(json.RawMessage(bad)); err == nil {
			t.Errorf("scope took %s", bad)
		}
	}
	if !slices.Contains(Kinds, KindEstimateDue) {
		t.Fatal("estimate_due is no kind")
	}
}
