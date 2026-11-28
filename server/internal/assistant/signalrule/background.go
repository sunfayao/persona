package signalrule

import (
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// StoppedBackground: background labels alone do not prove progress. Use observed activity where it
// exists, otherwise the start time. Without either, the parent's known stop bounds
// the age: its background work started before it stopped. A monitor with a future
// expiry is a timed wake. Activity timestamps also rearm deduplication.
func StoppedBackground(c proto.Session, now time.Time) (latest time.Time, waiting bool) {
	latest = c.UpdatedAt
	advance := func(at time.Time) {
		if at.After(latest) {
			latest = at
		}
	}
	advance(c.StateSince)
	if c.Work != nil {
		advance(c.Work.TurnAt)
	}
	if c.ActivityAt != nil {
		advance(*c.ActivityAt)
	}
	for _, b := range c.Background {
		if b.Kind == "monitor" && b.ExpiresAt != nil && b.ExpiresAt.After(now) {
			return latest, true
		}
		advance(b.StartedAt)
	}
	for _, a := range c.Subagents {
		if a.Running {
			advance(a.StartedAt)
			advance(a.UpdatedAt)
		}
	}
	for _, w := range c.Workflows {
		if w.Running || w.Status == proto.WorkflowRunning {
			at := w.StartedAt
			if w.UpdatedAt.After(at) {
				at = w.UpdatedAt
			}
			for _, a := range w.Agents {
				if a.StartedAt.After(at) {
					at = a.StartedAt
				}
				if a.UpdatedAt.After(at) {
					at = a.UpdatedAt
				}
			}
			advance(at)
		}
	}
	if w := c.Work; w != nil && (w.InteractionID != "" || w.WakeAt != nil && w.WakeAt.After(now)) {
		return latest, true
	}
	return latest, false
}
