package assistant

import (
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// wrongDoneWithin is the interval in which supervision counts a judge-closed to-do's reopening as a mistaken close.
const wrongDoneWithin = 24 * time.Hour

// Work events for reopened to-dos and supervised tasks sent back for rework.
const (
	// todoReopenedEvent: a done to-do was reopened, whichever way (by hand, a change asked on its "Please verify"
	// card, its agent set to work again). Detail: todo, done_by as it was, judge (closedByJudge), after (how many
	// seconds it had been done).
	todoReopenedEvent = "todo_reopened"
	// reviewReworkEvent: task_review sent a supervised task back (rework; one past the last round, which goes to the
	// user instead, too). Detail: todo, judged_done (it was waiting for review because the judge found it done).
	reviewReworkEvent = "review_rework"
)

// closedByJudge: done to-do t was closed by the hub's done judge, not by a person: on its own (done_by auto), or a
// supervised task whose check passed before #5732, which the judge accepted for its supervisor (its review by "judge").
func closedByJudge(t *Todo) bool {
	switch {
	case t.DoneBy == db.DoneByAuto:
		return true
	case t.DoneBy == db.DoneByReview && t.Supervision != nil:
		for _, r := range slices.Backward(t.Supervision.Reviews) {
			if r.Verdict == reviewAccepted {
				return r.By == reviewerJudge
			}
		}
	}
	return false
}

// reopenedDetail is the todoReopenedEvent of done to-do t (as it was) reopened by row.
func reopenedDetail(t *Todo, row db.TodoRow) map[string]any {
	at := row.UpdatedAt
	if row.ReopenedAt != nil {
		at = *row.ReopenedAt
	}
	return map[string]any{"todo": t.ID, "done_by": t.DoneBy, "judge": closedByJudge(t), "after": at.Sub(*t.DoneAt).Seconds(),
		"was_finished": t.StartError == "" && !t.Cancelled()}
}
