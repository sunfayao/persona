package assistant

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// Shortcuts the tests use into the assistant's own code paths, with the defaults a test wants. Production calls the
// functions underneath (triage, replyInboxFrom, linkTaskFor, acceptTry, inboxAgentFor) with its own arguments.

// len is how many cards have a lock kept.
func (c *cardLocks) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// waiting is how many hold or wait on card id's lock.
func (c *cardLocks) waiting(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l := c.m[id]; l != nil {
		return l.n
	}
	return 0
}

// triageOne is triage reporting only whether it changed the item.
func (s *Service) triageOne(ctx context.Context, it *InboxItem) bool { return s.triage(ctx, it) != nil }

// replyInbox is replyInboxFrom from the app with a derived send id.
func (s *Service) replyInbox(ctx context.Context, id, text string, changes ...DocChange) (InboxItem, error) {
	return s.replyInboxAs(ctx, id, text, "", changes...)
}

// replyInboxAs is the app's direct /reply route (not a message card).
func (s *Service) replyInboxAs(ctx context.Context, id, text, clientID string, changes ...DocChange) (InboxItem, error) {
	return s.replyInboxDoMode(ctx, id, text, clientID, false, true, true, changes...)
}

// linkTask is linkTaskFor on an item with no write pending.
func (s *Service) linkTask(ctx context.Context, it *InboxItem, tasks []prompts.InboxTask) bool {
	return s.linkTaskFor(ctx, it, tasks, nil)
}

// accepted is acceptTry without the error.
func (s *Service) accepted(ctx context.Context, id, card string) bool {
	ok, _ := s.acceptTry(ctx, id, card)
	return ok.taken
}

// agentArchiveChanges is when each of machine's archive records last changed, by session id. A later archive/restore
// supersedes an older delete tombstone. A subsequent explicit delete still works, without a non-atomic write to two
// settings keys.
func (svc *Service) agentArchiveChanges(machine string) map[string]time.Time {
	out := map[string]time.Time{}
	records := svc.agentArchives.snapshot.Load()
	if records == nil {
		return out
	}
	for _, rec := range *records {
		if rec.Session.MachineID == machine {
			out[rec.Session.SessionID] = rec.ChangedAt
		}
	}
	return out
}


// waitWorkEvents returns once the events recorded so far are in the store (or failed): what reads the stats right
// after acting sees them. The caller must know nothing else is recording events meanwhile (no Run,
// no triage goroutine from workTick), as a WaitGroup's Add from zero must not race its Wait.
func (s *Service) waitWorkEvents() { s.work.eventWrites.Wait() }
