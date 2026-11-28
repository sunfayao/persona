package assistant

import (
	"testing"
	"time"
)

// An automatic estimate extension is not a scope change: it neither restarts the overdue clock, moves its due time
// nor clears the overdue mark, and it does not count toward briefLogMax.
func TestAutoExtensionLeavesOverdueClockAlone(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	plain := &taskBrief{rec: briefRecord{CreatedAt: t0, ExpectMinutes: 30}}
	auto := BriefEvent{At: t0.Add(20 * time.Hour), Kind: briefEventEstimate, By: "hub", Auto: true, ExpectMinutes: 45}
	extended := &taskBrief{rec: plain.rec, st: briefState{Log: []BriefEvent{auto}}}
	if !extended.clockAt().Equal(plain.clockAt()) || !extended.dueAt().Equal(plain.dueAt()) {
		t.Fatalf("clock %v due %v, want %v and %v", extended.clockAt(), extended.dueAt(), plain.clockAt(), plain.dueAt())
	}
	marked := &taskBrief{rec: plain.rec, st: briefState{Log: []BriefEvent{
		{At: t0.Add(24 * time.Hour), Kind: briefEventOverdue},
		{At: t0.Add(25 * time.Hour), Kind: briefEventEstimate, By: "hub", Auto: true, ExpectMinutes: 45}}}}
	if marked.overdueAt() == nil {
		t.Fatal("an automatic extension cleared the overdue mark")
	}
	manual := &taskBrief{rec: plain.rec, st: briefState{Log: []BriefEvent{
		{At: t0.Add(24 * time.Hour), Kind: briefEventOverdue},
		{At: t0.Add(25 * time.Hour), Kind: briefEventEstimate, By: "conv1", ExpectMinutes: 45}}}}
	if manual.overdueAt() != nil || !manual.clockAt().Equal(t0.Add(25*time.Hour)) {
		t.Fatal("an estimate set by a supervisor starts another clock")
	}
}
