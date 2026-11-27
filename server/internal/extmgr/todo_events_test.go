package extmgr

import (
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// What the assistant says must have the fields the schema requires: anything else never reaches the extensions.
func TestTodoEventsRejectInvalidPayloads(t *testing.T) {
	h := newTestManager(t)
	// A nil registry would panic if a rejected payload reached the worker.
	h.extensions = &extensions{}
	for _, raw := range []string{`{}`, `[]`, `null`, `{"id":"t"}`, `{"id":"t","what":"renamed"}`, `{"title":"x","what":"updated"}`} {
		h.AssistantSaid(assistantwire.EventTodoChanged, json.RawMessage(raw))
	}
	for _, raw := range []string{`{}`, `[]`, `null`, `{"id":"t"}`, `{"id":"t","decision":{"source":"manual"}}`,
		`{"id":"t","decision":{"id":"d","source":"email"}}`, `{"decision":{"id":"d","source":"manual"}}`} {
		h.AssistantSaid(assistantwire.EventTodoDecision, json.RawMessage(raw))
	}
	q := &h.extensions.todoEvents
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running || len(q.pending) > 0 {
		t.Fatalf("an invalid event was queued: %+v", q.pending)
	}
}

// Nothing but the typed fields goes on: a field the assistant should not have sent is cut.
func TestTodoEventsPassOnOnlyTheirFields(t *testing.T) {
	h := newTestManager(t)
	h.extensions = &extensions{}
	holdEvents(&h.extensions.todoEvents) // the notice stays queued to be read
	h.AssistantSaid(assistantwire.EventTodoDecision, json.RawMessage(
		`{"id":"t1","title":"x","secret":"s","decision":{"id":"d1","question":"q","choice":"c","source":"manual","todo_id":"t1","source_session":"sess"}}`))
	q := &h.extensions.todoEvents
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) != 1 {
		t.Fatalf("queued: %+v", q.pending)
	}
	got, _ := json.Marshal(q.pending[0].payload)
	if string(got) != `{"decision":{"choice":"c","id":"d1","question":"q","source":"manual"},"id":"t1","title":"x"}` {
		t.Fatalf("payload: %s", got)
	}
}

// holdEvents keeps q's worker from starting: what is queued stays there to be read.
func holdEvents(q *extensionEvents) {
	q.mu.Lock()
	q.running = true
	q.mu.Unlock()
}

// With nobody subscribed and granted, nothing is queued.
func TestTodoEventsAreNotQueuedForNobody(t *testing.T) {
	h, _ := extensionTestHub(t)
	changed, _ := json.Marshal(assistantwire.TodoChangedEvent{ID: "todo_example_1", Title: "t", What: assistantwire.TodoUpdated})
	decision, _ := json.Marshal(assistantwire.TodoDecisionEvent{ID: "todo_example_1", Title: "t",
		Decision: assistantwire.TodoEventDecision{ID: "dec_example_1", Question: "q", Choice: "c", Source: "manual"}})
	if !h.AssistantSaid(assistantwire.EventTodoChanged, changed) || !h.AssistantSaid(assistantwire.EventTodoDecision, decision) {
		t.Fatal("the to-do events are not the extensions'")
	}
	q := &h.extensions.todoEvents
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running || len(q.pending) != 0 {
		t.Fatalf("queued for nobody: %+v", q.pending)
	}
}
