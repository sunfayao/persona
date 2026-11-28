package assistant

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/criteria"
	"github.com/sunfayao/persona/server/internal/db"
)

// criterionReport is an agent's reading of one criterion by its number (criteria.Report).
type criterionReport = criteria.Report

// applyChecks records only the criteria actually checked, without deriving any
// task verdict from their count. Unmatched checks remain in the review history.
func (v *Supervision) applyChecks(checks []ReviewCheck, at time.Time, by string) {
	v.Criteria = db.AlignTodoCriteria(v.DoneWhen, v.Criteria)
	for _, check := range checks {
		for i, text := range v.DoneWhen {
			if !criteria.Same(check.Criterion, text) {
				continue
			}
			state := "not_met"
			if check.Met {
				state = "met"
			}
			v.Criteria[i] = db.TodoCriterion{Criterion: text, State: state, Evidence: check.Evidence, At: &at, By: by}
			break
		}
	}
}

func (s *Service) recordCriterionReports(ctx context.Context, t Todo, machine, session string, reports []criterionReport, at time.Time) error {
	if len(reports) == 0 {
		return nil
	}
	moved := false // a reading changed: progress (supervise_progress.go)
	_, changed, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		moved = false
		// A concurrent criteria edit must not attach this report to a different entry.
		cur, ok := s.taskOf(ctx, machine, session)
		if !ok || cur.ID != t.ID || cur.DoneAt != nil || !slices.Equal(v.DoneWhen, t.Supervision.DoneWhen) {
			return false
		}
		v.Criteria = db.AlignTodoCriteria(v.DoneWhen, v.Criteria)
		for _, r := range reports {
			text := v.DoneWhen[r.Number-1]
			if old := v.Criteria[r.Number-1]; old.State != r.State || old.Evidence != r.Evidence {
				moved = true
			}
			v.Criteria[r.Number-1] = db.TodoCriterion{Criterion: text, State: r.State, Evidence: r.Evidence,
				At: &at, By: "agent:" + machine + "/" + session}
		}
		return true
	})
	if err == nil && !changed {
		return fmt.Errorf("task or done_when changed: read the current task and report again")
	}
	if err == nil && moved {
		s.progressMade(machine, session, at)
	}
	return err
}
