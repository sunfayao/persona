package assistant

import (
	"testing"
	"time"
)

func estimateCuePending(s *Service, todo string) bool {
	s.supervise.mu.Lock()
	defer s.supervise.mu.Unlock()
	_, ok := s.supervise.pending[todo+"|"+sigEstimateDue]
	return ok
}

// sawProgress records real progress of the task's Agent at at (what the no_progress watch keeps).
func sawProgress(s *Service, todo Todo, at time.Time) {
	a := todo.Agents[0]
	s.progress.mu.Lock()
	defer s.progress.mu.Unlock()
	if s.progress.m == nil {
		s.progress.m = map[[2]string]*spinWatch{}
	}
	s.progress.m[[2]string{a.MachineID, a.SessionID}] = &spinWatch{progressAt: at}
}

// A task whose Agent keeps making progress has its due estimate extended by half, silently, twice; the third time it is
// raised. The extensions are logged on the brief and keep the review clock where it started.
func TestProgressingTaskExtendsItsEstimateTwiceThenRaises(t *testing.T) {
	s, _, todo, brief := startEstimateTask(t, nil, map[string]any{"expect_minutes": 30})
	ctx := t.Context()
	clock := brief.EstimateClockAt
	for i, want := range []int{45, 67} {
		now := brief.EstimateDueAt
		sawProgress(s, todo, now.Add(-time.Minute))
		s.briefClock(ctx, todo, now)
		brief = s.mustBrief(t, todo.ID)
		if estimateCuePending(s, todo.ID) || brief.ExpectNow != want || brief.EstimateAutoExtended != i+1 ||
			!brief.EstimateClockAt.Equal(clock) || !brief.EstimateDueAt.Equal(clock.Add(time.Duration(want)*time.Minute)) {
			t.Fatalf("extension %d: raised %v, %+v", i+1, estimateCuePending(s, todo.ID), brief)
		}
	}
	if log := briefEventsOf(brief, briefEventEstimate); len(log) != 2 || !log[0].Auto || log[0].Change == "" || log[0].ExpectMinutes != 45 {
		t.Fatalf("brief log: %+v", log)
	}
	if v := todoNow(s, todo.ID).Supervision; v == nil || v.ExpectMinutes != 67 {
		t.Fatalf("ledger estimate: %+v", v)
	}
	sawProgress(s, todo, brief.EstimateDueAt.Add(-time.Minute))
	s.briefClock(ctx, todo, brief.EstimateDueAt)
	if !estimateCuePending(s, todo.ID) || s.mustBrief(t, todo.ID).ExpectNow != 67 {
		t.Fatal("the third due estimate was not raised")
	}
}

// An idle Agent (no progress inside the window, a quarter of the estimate or ten minutes) is raised at once.
func TestIdleTaskRaisesItsDueEstimateAtOnce(t *testing.T) {
	s, _, todo, brief := startEstimateTask(t, nil, map[string]any{"expect_minutes": 30})
	sawProgress(s, todo, brief.EstimateDueAt.Add(-time.Hour))
	s.briefClock(t.Context(), todo, brief.EstimateDueAt)
	if !estimateCuePending(s, todo.ID) || s.mustBrief(t, todo.ID).EstimateAutoExtended != 0 {
		t.Fatal("an idle task was extended instead of raised")
	}
}

// An estimate a supervisor or the owner sets starts the count again.
func TestNewEstimateResetsTheAutomaticExtensions(t *testing.T) {
	s, _, todo, brief := startEstimateTask(t, nil, map[string]any{"expect_minutes": 30})
	ctx := t.Context()
	sawProgress(s, todo, brief.EstimateDueAt.Add(-time.Minute))
	s.briefClock(ctx, todo, brief.EstimateDueAt)
	brief = s.mustBrief(t, todo.ID)
	if brief.EstimateAutoExtended != 1 {
		t.Fatalf("not extended: %+v", brief)
	}
	if _, err := s.applyScope(ctx, todo, scopeChange{By: "conv1", Change: "more time", Why: "CI is slow", Expect: 60}); err != nil {
		t.Fatal(err)
	}
	brief = s.mustBrief(t, todo.ID)
	if brief.EstimateAutoExtended != 0 || brief.ExpectNow != 60 {
		t.Fatalf("the count was not reset: %+v", brief)
	}
	for i := range 2 {
		sawProgress(s, todo, brief.EstimateDueAt.Add(-time.Minute))
		s.briefClock(ctx, todo, brief.EstimateDueAt)
		brief = s.mustBrief(t, todo.ID)
		if estimateCuePending(s, todo.ID) || brief.EstimateAutoExtended != i+1 {
			t.Fatalf("extension %d after the reset: %+v", i+1, brief)
		}
	}
}
