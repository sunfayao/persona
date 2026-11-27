package assistant

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The task lock (s.tasks.mu) is the package's hot lock: starting tasks, routing messages, cards, stages, the
// workspace. The turns' ledger is read and written in the database with its own mutex held (task_turns.go), so
// nothing may wait for that mutex while it holds the task lock: one ledger write that does not come back would park
// every one of those behind it, for as long as the write's context allows (#5718, the second review).

// ledgerStalls is a database whose write of the turns' ledger does not come back while stalled is set: a pool with
// no connection left, a Postgres that restarts. waits gets a value when such a write arrives; answer, once closed,
// lets them through.
type ledgerStalls struct {
	*memStore
	stalled atomic.Bool
	waits   chan struct{}
	answer  chan struct{}
}

func (m *ledgerStalls) PutSetting(ctx context.Context, key string, v any) error {
	if key == taskTurnsKey && m.stalled.Load() {
		select {
		case m.waits <- struct{}{}:
		default:
		}
		select {
		case <-m.answer:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.memStore.PutSetting(ctx, key, v)
}

// comesBack runs do and says whether it returned within d. One that did not is still running: the test lets go of
// what it waits for before it ends.
func comesBack(d time.Duration, do func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		do()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// With a ledger write stuck in the database, what reads or sets the "an end is being handled" mark under the task
// lock still comes back at once, and so does a caller that needs the task lock alone.
func TestTaskLockDoesNotWaitForALedgerWrite(t *testing.T) {
	st := &ledgerStalls{memStore: &memStore{}, waits: make(chan struct{}, 1), answer: make(chan struct{})}
	s, _ := newTaskServiceOn(t, st)
	ctx := context.Background()
	s.turnOf(ctx, "") // the ledger is read, and kept for the first time, while the database still answers
	var once sync.Once
	answers := func() { once.Do(func() { close(st.answer) }) }
	defer answers()

	st.stalled.Store(true)
	wrote := make(chan struct{})
	go func() { // a turn is taken up, and its ledger write does not come back
		defer close(wrote)
		s.takeTurn(ctx, Todo{ID: "todo_a"}, turnRow(proto.StatusIdle, 3, "修好了"), nil, turnClose{Key: "turn-1"}, true, time.Now(), turnDone)
	}()
	select {
	case <-st.waits:
	case <-time.After(3 * time.Second):
		t.Fatal("the ledger write did not reach the database")
	}
	for _, c := range []struct {
		what string
		do   func()
	}{
		{"a look asking whether a start was heard, under the task lock (turnStartHeard)", func() { s.turnStartHeard("todo_b") }},
		{"an end's mark, set with its reservation under the task lock (taskJudging)", func() {
			s.taskJudging("todo_b", 1)
			s.taskJudging("todo_b", -1)
		}},
		{"a caller that needs the task lock alone (taskWorkCurrent)", func() { s.taskWorkCurrent("todo_b", time.Now()) }},
	} {
		if !comesBack(time.Second, c.do) {
			t.Errorf("%s waited for the ledger's database write", c.what)
		}
	}
	select {
	case <-wrote:
		t.Fatal("the ledger write came back while the database did not answer: nothing was held")
	default:
	}

	answers()
	select {
	case <-wrote:
	case <-time.After(3 * time.Second):
		t.Fatal("the ledger write did not come back once the database answered")
	}
	if e, ok := s.turnOf(ctx, "todo_a"); !ok || e.Key != "turn-1" || e.State != turnDone {
		t.Fatalf("the turn on the ledger after the write = %+v, %v", e, ok)
	}
}
