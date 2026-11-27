package assistant

import (
	"testing"
	"time"
)

// turnAt is a turn that came at since, behind prev, read on a clock that says now.
func turnAt(prev chan struct{}, wait time.Duration, since, now time.Time) *feishuTurn {
	return &feishuTurn{prev: prev, done: make(chan struct{}), wait: wait, since: since, now: func() time.Time { return now }}
}

func closedWithin(c chan struct{}, d time.Duration) bool {
	select {
	case <-c:
		return true
	case <-time.After(d):
		return false
	}
}

// A message that came longer ago than the wait goes on at once, though the one before it is still stuck (#4846).
func TestFeishuTurnPastItsWaitGoesOnAtOnce(t *testing.T) {
	t0 := time.Unix(1000, 0)
	stuck := make(chan struct{})
	turn := turnAt(stuck, time.Hour, t0, t0.Add(2*time.Hour))
	turn.pass()
	if !closedWithin(turn.done, 2*time.Second) {
		t.Fatal("a message past its wait still waits for the stuck one")
	}
}
