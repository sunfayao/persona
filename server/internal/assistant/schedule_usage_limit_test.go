package assistant

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A scheduled run whose conversation a subscription's usage limit stopped (docs/assistant/agent-work.md "Usage
// limits"): failed and pushed as ever, retried once the limit reset rather than after runRetryDelay (when it would be
// refused again), not at all when its CLI goes on by itself.

func retried(h *Service) bool {
	sc := h.loadSchedule(context.Background())
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return slices.ContainsFunc(sc.runs, func(r ScheduleRun) bool { return r.RefID == "p" && r.Retried })
}

// Without a stand-in, the retry goes a minute or so after the reset, and from now when the reset is already past.
func TestLimitRetryWait(t *testing.T) {
	h := newStorelessService(t, newFakeHub())
	now := time.Now()
	for _, reset := range []time.Time{now.Add(2 * time.Hour), now.Add(-time.Hour)} {
		d := h.limitRetryWait(reset, now)
		from := max(reset.Sub(now), 0)
		if d < from+limitContinueAfter || d >= from+limitContinueAfter+limitContinueSpread {
			t.Errorf("reset %v from now: waits %v", reset.Sub(now), d)
		}
	}
}
