package assistant

import (
	"testing"
	"time"
)

// retryCancelled on a fixed clock: the retry of a failed run is due at retryAt, three minutes after the failure.
func TestRetryCancelledWhenTheOwnerActedAfterTheFailure(t *testing.T) {
	failedAt := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	retryAt := failedAt.Add(runRetryDelay)
	failed := ScheduleRun{RefID: "fu", At: failedAt, Status: runFailed}
	oneOff := Task{ID: "fu", Repeat: repeatOnce, At: failedAt, Enabled: false, LastRun: failedAt} // off once it was due (runDue)
	at := func(t Task, when time.Time) Task { t.At, t.Enabled = when, true; return t }
	ran := func(r ScheduleRun) []ScheduleRun { return []ScheduleRun{failed, r} }

	cases := []struct {
		name   string
		cur    Task
		found  bool
		runs   []ScheduleRun
		cancel bool
	}{
		{"nothing done since: retried", oneOff, true, []ScheduleRun{failed}, false},
		{"Run now since: not retried", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Manual: true, Status: runOK, SessionID: "s1"}), true},
		{"Run now whose turn then failed: it still went out", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Manual: true, Status: runFailed, SessionID: "s1"}), true},
		{"Run now that could not start: retried", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Manual: true, Status: runFailed}), false},
		{"Run now with lost receipt: not retried", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Manual: true, Status: runFailed, DeliveryUnknown: true}), true},
		{"Run now skipped (busy): retried", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Manual: true, Status: runSkipped, SessionID: "s1"}), false},
		{"another task's run since: retried", oneOff, true,
			ran(ScheduleRun{RefID: "other", At: failedAt.Add(time.Minute), Manual: true, Status: runOK, SessionID: "s2"}), false},
		{"a run before the failed one: retried", oneOff, true,
			[]ScheduleRun{{RefID: "fu", At: failedAt.Add(-time.Hour), Status: runOK, SessionID: "s0"}, failed}, false},
		{"moved later: not retried", at(oneOff, failedAt.Add(time.Hour)), true, []ScheduleRun{failed}, true},
		{"moved to tomorrow: not retried", at(oneOff, failedAt.Add(24*time.Hour)), true, []ScheduleRun{failed}, true},
		// Moved earlier than it was set before (#5320 counts only where the time is now): still ahead of the retry,
		// it runs then.
		{"moved earlier, still after the retry: not retried", at(Task{ID: "fu", Repeat: repeatOnce, LastRun: failedAt}, retryAt.Add(time.Minute)),
			true, []ScheduleRun{failed}, true},
		// Moved to a time before the retry: the scheduler ran it then, which is a run since the failure.
		{"moved earlier, before the retry, and run then: not retried", oneOff, true,
			ran(ScheduleRun{RefID: "fu", At: failedAt.Add(time.Minute), Status: runOK, SessionID: "s1"}), true},
		{"deleted: not retried", Task{}, false, []ScheduleRun{failed}, true},
		// A repeating task's next time is always ahead (an interval one's moved on as it ran): that is no move.
		{"interval task, its next time ahead: retried", Task{ID: "fu", Repeat: repeatInterval, EveryMinutes: 30, At: failedAt.Add(30 * time.Minute), Enabled: true},
			true, []ScheduleRun{failed}, false},
		{"daily task: retried", Task{ID: "fu", Repeat: repeatDaily, Time: "09:00", Enabled: true}, true, []ScheduleRun{failed}, false},
		{"repeating task switched off: not retried", Task{ID: "fu", Repeat: repeatDaily, Time: "09:00"}, true, []ScheduleRun{failed}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := retryCancelled(c.cur, c.found, failed, c.runs, retryAt)
			if (why != "") != c.cancel {
				t.Fatalf("cancel %v, got %q", c.cancel, why)
			}
		})
	}
}
