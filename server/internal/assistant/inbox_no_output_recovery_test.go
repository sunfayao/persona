package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

func flushInboxRecoveryDigest(s *Service) {
	b := &s.supervise
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	for id, g := range b.pending {
		g.At = time.Now().Add(-2 * time.Hour)
		b.pending[id] = g
	}
	b.last = time.Time{}
	b.mu.Unlock()
	s.sendDigest(context.Background())
}

func TestInboxNoOutputUnconfirmedNoticeSurvivesAnOwnerWait(t *testing.T) {
	ctx := context.Background()
	s, f, todo := inboxRetryService(t, todoTestDB(t))
	s.inboxToAgent(ctx, inboxRetryEvent(todo.ID, "review-owner-wait"))
	sent := sentToAgent(f)
	inboxRetryTurn(s, f, "empty-before-wait", sent[len(sent)-1], "", 10)
	sends := len(sentToAgent(f))
	// Model a manager stopped after reserving the retry but before saving its outcome.
	if err := s.updateTaskInboxEvents(ctx, func(events map[string]taskInboxEvent) bool {
		for id, event := range events {
			if event.Todo == todo.ID && event.Retried {
				event.Failure = inboxRetryUnconfirmed
				event.RetryAt = time.Now().Add(-6 * time.Minute)
				events[id] = event
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatal(err)
	}
	s.recoverTaskInboxEvents(ctx)
	pending := unhandledInboxSignals(s)
	if len(pending) != 1 || pending[0].urgent() {
		t.Fatalf("the expired unconfirmed send lost its default-urgency source: %+v", pending)
	}
	failure := pending[0]
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
		v.State = reviewNeedsUser
		return true
	}); err != nil {
		t.Fatal(err)
	}
	flushInboxRecoveryDigest(s)
	if len(f.notices()) != 0 {
		t.Fatal("the unconfirmed event bypassed the owner's decision wait")
	}
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
		v.State = ""
		return true
	}); err != nil {
		t.Fatal(err)
	}
	s.recoverTaskInboxEvents(ctx)
	pending = unhandledInboxSignals(s)
	if len(pending) != 1 || pending[0].Key != failure.Key {
		t.Fatalf("the early sent marker permanently consumed an owner-held source: %+v", pending)
	}
	flushInboxRecoveryDigest(s)
	notices := f.notices()
	if len(notices) != 1 || !strings.Contains(notices[0], failure.Inbox.URL) ||
		!strings.Contains(notices[0], failure.Inbox.Sender) {
		t.Fatalf("the resolved owner wait lost the source-bearing escalation: %q", notices)
	}
	s.recoverTaskInboxEvents(ctx)
	flushInboxRecoveryDigest(s)
	if len(f.notices()) != 1 || len(sentToAgent(f)) != sends {
		t.Fatal("restoring the source repeated its escalation or spent another retry")
	}
}
