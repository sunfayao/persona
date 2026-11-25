package assistant

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func waitFleetBackground(t *testing.T, name string, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("%s did not finish before the database fixture closed", name)
	}
}

// orphanEndMem observes the real grace callback's outcome write without closing a database under a finished test.
type orphanEndMem struct {
	*fleetMem
	ended chan string
}

// delayedBackgroundStore lets a background database call finish only after its test has returned. A fixture that drains
// the call before closing the database sees no error; without that drain, the closed-pool error logs through the ended T.
type delayedBackgroundStore struct {
	*db.DB
	release chan struct{}
}

// acceptedSendStore pauses an accepted send record while it reads the conversation roles. The test can then stop new
// scheduling before releasing the record, proving that the rest of that accepted operation still writes its event.
type acceptedSendStore struct {
	*db.DB
	rolesStarted chan struct{}
	releaseRoles chan struct{}
	startedOnce  sync.Once
}

// digestLoadStore holds a digest in its todo restoration, before it takes the delivery lock. Draining still has to
// wait for that callback; otherwise the database fixture can close while the callback is only getting started.
type digestLoadStore struct {
	*db.DB
	started chan struct{}
	release chan struct{}
	loaded  chan struct{}
	once    sync.Once
}

// drainFleetBackground stops new background scheduling, then lets active calls finish before a test closes its store.
// Production Run uses the same canceled runCtx to stop new naming passes and direction-tree timers.
func drainFleetBackground(t *testing.T, s *Service, cancel context.CancelFunc) {
	t.Helper()
	cancel()

	// A retry callback may still be restoring todos before it takes dedupMu. Cancel its context, wait for the whole
	// callback, then disarm anything left; a callback that was already due observes the cancellation before any I/O.
	digest := &s.supervise
	digest.digestMu.Lock()
	digest.mu.Lock()
	if digest.timer != nil {
		digest.timer.Stop()
		digest.timer, digest.timerAt = nil, time.Time{}
	}
	digest.mu.Unlock()
	digest.digestMu.Unlock()

	names := &s.agentNames
	names.mu.Lock()
	if names.timer != nil {
		names.timer.Stop()
		names.timer = nil
		names.timerSeq++
	}
	names.mu.Unlock()

	s.todoViewsStop()

	// A screen-upgrade start checks runCtx under fleet.mu; taking it here closes the Add/Wait teardown window.
	// Delayed turn completions use the same gate: stop pending timers and let an already running callback finish.
	s.fleet.mu.Lock()
	s.fleet.completionStopped = true
	for timer := range s.fleet.completionTimers {
		if timer.Stop() {
			s.fleet.completionRuns.Done()
		}
		delete(s.fleet.completionTimers, timer)
	}
	for _, p := range s.fleet.interactions {
		if p.orphaned != nil {
			p.orphaned.Stop()
			p.orphaned = nil // a queued callback checks this under fleet.mu before recording its outcome
		}
	}
	s.fleet.mu.Unlock()
	// workEvent and recordSendAsync use work.mu for the same runCtx + Add scheduling gate.
	s.work.mu.Lock()
	s.work.mu.Unlock()

	waitFleetBackground(t, "turn completions", &s.fleet.completionRuns)
	waitFleetBackground(t, "agent naming", &names.runs)
	waitFleetBackground(t, "to-do views", &s.tview.runs)
	waitFleetBackground(t, "screen upgrades", &s.fleet.upgrades)
	waitFleetBackground(t, "work events", &s.work.eventWrites)
}
