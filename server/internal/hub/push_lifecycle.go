package hub

import (
	"context"
	"time"
)

// pushMaintenance is how often the delivered-push record is mirrored (var for tests).
var pushMaintenance = 5 * time.Minute

// maintenanceLoop is tied to hub shutdown.
func (p *Pusher) maintenanceLoop(ctx context.Context) {
	if p.store == nil {
		return
	}
	tick := time.NewTicker(pushMaintenance)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		p.mirrorSentIfDue(ctx) // the delivered pushes for a build from before hub_push_sent (push_grading.go)
	}
}

// resumeWrites wakes held/sent push persistence and Web Push after a snapshot pause ends (quiesce.go).
func (p *Pusher) resumeWrites() {
	if p == nil {
		return
	}
	p.gradingMu.Lock()
	if p.store != nil && !p.gradingWriting && (p.heldDirty || len(p.sentPending) > 0) {
		p.gradingWriting = true
		go p.saveIntercepts()
	}
	p.gradingMu.Unlock()
	p.web.resumeWrites()
}
