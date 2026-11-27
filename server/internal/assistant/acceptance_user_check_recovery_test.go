package assistant

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
)

type userCheckPassStore struct {
	*db.DB
	closeCalls       atomic.Int32
	ledgerWrites     atomic.Int32
	acceptanceWrites atomic.Int32
	stageWrites      atomic.Int32
	failLedger       atomic.Bool
}

func TestUserCheckPassWaitsForItsOwnRound(t *testing.T) {
	for _, source := range []string{"web button", "card reply", "recovery sweep"} {
		t.Run(source, func(t *testing.T) {
			ctx := t.Context()
			s, _, todo := acceptedForUserCheck(t)
			old := askOf(s, todo.ID)
			actions := &s.reviewDismissals.actions
			actions.acquire(todo.ID)
			locked := true
			defer func() {
				if locked {
					actions.release(todo.ID)
				}
			}()
			if source == "recovery sweep" {
				s.accept.mu.Lock()
				s.accept.asks[todo.ID].AcceptedAt = time.Now().UTC()
				s.accept.mu.Unlock()
			}
			finished := make(chan error, 1)
			go func() {
				switch source {
				case "recovery sweep":
					s.verifySupervisedAsks(ctx)
					finished <- nil
				case "card reply":
					s.acceptReply(ctx, "om_reply", "Pass", false, old.MessageID)
					finished <- nil
				default:
					_, err := s.acceptTryFromBrief(ctx, todo.ID, old.AskedAt.Format(timeKeyLayout))
					finished <- err
				}
			}()
			waitFor(t, "the old Pass queued behind the task action", func() bool {
				actions.mu.Lock()
				defer actions.mu.Unlock()
				return actions.m[todo.ID].n == 2
			})
			fresh := &acceptAsk{AskedAt: time.Now().UTC(), MessageID: "om_fresh", Messages: []string{"om_fresh"}}
			s.accept.mu.Lock()
			s.accept.asks[todo.ID] = fresh
			s.accept.mu.Unlock()
			actions.release(todo.ID)
			locked = false
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the old Pass did not return")
			}
			got, current := todoNow(s, todo.ID), askOf(s, todo.ID)
			if got.DoneAt != nil || current == nil || !current.AcceptedAt.IsZero() {
				t.Fatalf("a previous Pass affected the new verification round: done=%v ask=%+v", got.DoneAt, current)
			}
		})
	}
}

func (d *userCheckPassStore) ReviewDoneTodo(ctx context.Context, id, reason string, accepted bool, at time.Time) (*db.TodoRow, bool, error) {
	d.closeCalls.Add(1)
	return d.DB.ReviewDoneTodo(ctx, id, reason, accepted, at)
}

func (d *userCheckPassStore) PutTodoSupervision(ctx context.Context, row db.TodoSupervisionRow) error {
	if row.State == reviewAccepted {
		d.ledgerWrites.Add(1)
		if d.failLedger.Load() {
			return errors.New("supervision store unavailable")
		}
	}
	return d.DB.PutTodoSupervision(ctx, row)
}

func (d *userCheckPassStore) PutSetting(ctx context.Context, key string, value any) error {
	switch key {
	case acceptanceKey:
		d.acceptanceWrites.Add(1)
	case reportStagesKey:
		d.stageWrites.Add(1)
	}
	return d.DB.PutSetting(ctx, key, value)
}

func TestUserCheckRetriesFailedPassLedgerBeforeVerification(t *testing.T) {
	ctx := context.Background()
	store := &userCheckPassStore{DB: todoTestDB(t)}
	s, f, todo := acceptedForUserCheckOn(t, store)
	updated := updatedCards(f)
	store.failLedger.Store(true)
	if result, err := s.acceptTry(ctx, todo.ID, ""); result.taken || !errors.Is(err, errAcceptStore) {
		t.Fatalf("failed ledger update reported a completed Pass: %+v, %v", result, err)
	}
	done := todoNow(s, todo.ID)
	if done.DoneAt == nil || done.Supervision.State != reviewUserCheck || done.stageVerified {
		t.Fatalf("failed ledger update skipped ahead to verified: %+v", done)
	}
	a := askOf(s, todo.ID)
	if a == nil || a.AcceptedAt.IsZero() || len(updated()) != 0 {
		t.Fatalf("durable Pass lost or prematurely redrawn: ask=%+v, cards=%d", a, len(updated()))
	}
	store.closeCalls.Store(0)
	store.acceptanceWrites.Store(0)
	store.stageWrites.Store(0)
	restarted := newServiceOn(t, f, store)
	restarted.verifySupervisedAsks(ctx)
	if got := todoNow(restarted, todo.ID); got.Supervision.State != reviewUserCheck || got.stageVerified {
		t.Fatalf("failed restart skipped the pending ledger write: %+v", got)
	}
	store.failLedger.Store(false)
	restarted.verifySupervisedAsks(ctx)
	got := todoNow(restarted, todo.ID)
	if got.Supervision.State != reviewAccepted || !got.stageVerified || !got.DoneAt.Equal(*done.DoneAt) {
		t.Fatalf("ledger recovery did not finish the original Pass: %+v", got)
	}
	if store.closeCalls.Load() != 0 || store.acceptanceWrites.Load() != 0 || store.stageWrites.Load() != 1 || len(updated()) != 1 {
		t.Fatalf("recovery repeated durable steps: closes=%d asks=%d stages=%d cards=%d",
			store.closeCalls.Load(), store.acceptanceWrites.Load(), store.stageWrites.Load(), len(updated()))
	}
}
