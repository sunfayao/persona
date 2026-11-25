package assistant

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// What the extensions are told of the todos (todo_events.go, #5279): todo.changed for a created, deleted or changed
// todo (changes within a second are one), todo.decision for every decision written. All ids are placeholders.

// todoEventHub is a hub that carries extensions: it takes the todo events.
type todoEventHub struct {
	*fakeHub
	mu    sync.Mutex
	sent  []recordedEvent
	block chan struct{} // when set, the hub takes no event until it is closed
}

func (f *todoEventHub) TodoEvent(event string, params any) error {
	if f.block != nil {
		<-f.block
	}
	raw, _ := json.Marshal(params)
	f.mu.Lock()
	f.sent = append(f.sent, recordedEvent{event, raw})
	f.mu.Unlock()
	return nil
}

type todoEventEnv struct {
	*todoEnv
	hub   *todoEventHub
	clock *fakeClock
}

// told waits for the queue to reach the hub and returns what it has been told since the last call.
func (e *todoEventEnv) told() []recordedEvent {
	e.t.Helper()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
		q := &e.s.tevents
		q.qmu.Lock()
		idle := !q.running && len(q.pending) == 0
		q.qmu.Unlock()
		if idle {
			e.hub.mu.Lock()
			defer e.hub.mu.Unlock()
			out := e.hub.sent
			e.hub.sent = nil
			return out
		}
	}
	e.t.Fatal("the todo events did not settle")
	return nil
}

// A hub with no extensions (the in-process tests' fake, an embedder without them) gets nothing and nothing waits.
func TestTodoEventsAreOffWithoutAPublisher(t *testing.T) {
	e := newTodoEnv(t, nil)
	e.create("发版")
	if e.s.todoEventsOn() {
		t.Fatal("the fake hub takes todo events")
	}
	e.s.tevents.mu.Lock()
	defer e.s.tevents.mu.Unlock()
	if e.s.tevents.armed {
		t.Fatal("a flush was scheduled with nobody to tell")
	}
}

// A backlog (the hub slow to take events) is at most one todo.changed a todo, a todo made and deleted before the hub
// took the first is none, and past the limit the oldest goes.
func TestTodoEventBacklogIsMergedAndBounded(t *testing.T) {
	f := &todoEventHub{fakeHub: newFakeHub().withMachine(), block: make(chan struct{})}
	s := withSettings(t, f.fakeHub, New(Options{Hub: f, Logf: testLogf(t)}))
	released := false
	release := func() {
		if !released {
			released = true
			close(f.block)
		}
	}
	defer release()
	s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{ID: "todo_example_first", Title: "x", What: "created"}})
	// The worker holds that first one at the hub (it has left the queue when this sees it empty); what comes now waits.
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(time.Millisecond) {
		s.tevents.qmu.Lock()
		taken := len(s.tevents.pending) == 0
		s.tevents.qmu.Unlock()
		if taken {
			break
		}
	}
	const todos = 400
	for i := range todos {
		id := fmt.Sprintf("todo_example_%d", i)
		for _, what := range []string{assistantwire.TodoCreated, assistantwire.TodoUpdated, assistantwire.TodoUpdated} {
			s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{ID: id, Title: "t", What: what, Done: what == "updated"}})
		}
		if i%2 == 1 {
			s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{ID: id, Title: "t", What: assistantwire.TodoDeleted}})
		}
	}
	q := &s.tevents
	q.qmu.Lock()
	n, dropped := len(q.pending), q.dropped
	q.qmu.Unlock()
	if n != todos/2 || dropped {
		t.Fatalf("%d events wait for %d todos (half deleted unseen), dropped %v", n, todos, dropped)
	}
	q.qmu.Lock()
	for _, p := range q.pending {
		if c := p.params.(assistantwire.TodoChangedEvent); c.What != assistantwire.TodoCreated || !c.Done {
			t.Errorf("not one created event with the latest state: %+v", c)
		}
	}
	q.pending = nil
	q.qmu.Unlock()

	for i := range todoEventLimit + 5 {
		s.todoQueue(todoNotice{assistantwire.EventTodoDecision, assistantwire.TodoDecisionEvent{ID: "todo_example_1", Title: "t",
			Decision: assistantwire.TodoEventDecision{ID: fmt.Sprintf("dec_example_%d", i), Source: "manual"}}})
	}
	q.qmu.Lock()
	n, dropped = len(q.pending), q.dropped
	first := q.pending[0].params.(assistantwire.TodoDecisionEvent).Decision.ID
	q.qmu.Unlock()
	if n != todoEventLimit || !dropped || first != "dec_example_5" {
		t.Fatalf("past the limit: %d waiting, dropped %v, the oldest %s", n, dropped, first)
	}
	release()
	e := &todoEventEnv{todoEnv: &todoEnv{t: t, s: s, f: f.fakeHub}, hub: f}
	e.told()
	q.qmu.Lock()
	defer q.qmu.Unlock()
	if q.dropped {
		t.Fatal("the dropped mark stays after the queue is empty")
	}
}

// recordedEvent is one event an event-capable hub took.
type recordedEvent struct {
	name string
	raw  json.RawMessage
}
