package assistant

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// countingPlanHistoryStore counts the plan execution history reads through it.
type countingPlanHistoryStore struct {
	Store
	reads atomic.Int64
}

// A task that runs on an existing to-do through task_plan's todo_id keeps that to-do's plan id, which a piece of a
// leader's direction (todo_create) does not have: only the plan task's todo_id binds it to the plan. When its agent does
// not start, task_retry, the HTTP retry and the start remedies find that plan as they find a continuation's (#8770); a
// to-do no started plan binds, or no conversation opened, is still not started again. Ids are placeholders.
