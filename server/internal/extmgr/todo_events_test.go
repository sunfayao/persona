package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/extension"
)

// The to-do events (#5279) reach a running extension only when it subscribed to the topic and was granted todos.read
// (or the legacy full grant): todos.write, no scope, or the scope without the subscription gets nothing. (The two
// events are not told in any order: the extension's handlers may run at once.) All ids are placeholders.
func TestTodoEventsReachOnlyAuthorizedExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, permissions string
		subscribe         []string
		allowed           bool
	}{
		{"reader", `{"hub":["todos.read"]}`, []string{"todo.changed", "todo.decision", "agent.finished"}, true},
		{"reader-changed-only", `{"hub":["todos.read"]}`, []string{"todo.changed", "agent.finished"}, true},
		{"writer", `{"hub":["todos.write"]}`, []string{"todo.changed", "todo.decision", "agent.finished"}, false},
		{"kb-reader", `{"hub":["agents.read"]}`, []string{"todo.changed", "todo.decision", "agent.finished"}, false},
		{"none", `{}`, []string{"todo.changed", "todo.decision", "agent.finished"}, false},
		{"legacy", `{"hub_api":true}`, []string{"todo.changed", "todo.decision", "agent.finished"}, true},
		{"reader-not-subscribed", `{"hub":["todos.read"]}`, []string{"agent.finished", "inbox.changed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := extensionTestHub(t)
			mux := h.routes()
			token := h.cfg.Token
			dir := uiPackage(t)
			path := filepath.Join(dir, extension.ManifestFile)
			raw, _ := os.ReadFile(path)
			var manifest map[string]any
			_ = json.Unmarshal(raw, &manifest)
			var permissions map[string]any
			_ = json.Unmarshal([]byte(tc.permissions), &permissions)
			permissions["launcher_keys"] = true
			manifest["permissions"] = permissions
			manifest["contributes"].(map[string]any)["events"] = []any{map[string]any{"id": "todos", "subscribe": tc.subscribe}}
			raw, _ = json.Marshal(manifest)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", token, `{"source":"dir:`+dir+`"}`)
			if code != 200 {
				t.Fatalf("preview %d %v", code, pv)
			}
			code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", token, fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, pv["preview_id"], pv["permissions_digest"]))
			if code != 200 {
				t.Fatalf("install %d %v", code, out)
			}
			waitLong(t, "to-do subscriber running", func() bool { p := h.extensions.get("example-ui"); return p != nil && p.proc != nil && p.proc.Ready() })
			changed, _ := json.Marshal(assistantwire.TodoChangedEvent{ID: "todo_example_1", Title: "Ship the release", What: assistantwire.TodoUpdated, Done: true,
				Thread: &assistantwire.TodoEventThread{Channel: "feishu", ChatID: "oc_example_1", ThreadID: "omt_example_1", Title: "Release 1.3"}})
			decision, _ := json.Marshal(assistantwire.TodoDecisionEvent{ID: "todo_example_1", Title: "Ship the release",
				Decision: assistantwire.TodoEventDecision{ID: "dec_example_1", Question: "Which branch", Choice: "release-1.3", Source: "thread"}})
			// The assistant tells of its todos in this process (AssistantSaid), in order.
			if !h.AssistantSaid(assistantwire.EventTodoChanged, changed) || !h.AssistantSaid(assistantwire.EventTodoDecision, decision) {
				t.Fatal("the to-do events are not the extensions'")
			}
			view := func() string {
				_, out := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/home", token, `{}`)
				return fmt.Sprint(out["view"])
			}
			// The worker has handed both to the extension before the sentinel goes: the sentinel is told straight, not
			// through the queue, so without waiting it could overtake them and a leak would go unseen.
			waitEventQueuesEmpty(t, h)
			if !tc.allowed {
				// The queue is not told of what nobody is to hear, so the check in emit is not reached that way: tell
				// emit straight, as the worker would, to hold that check itself.
				h.extensions.emit(assistantwire.EventTodoChanged, map[string]any{"id": "todo_example_1", "title": "Ship the release", "what": "updated"})
				h.extensions.emit(assistantwire.EventTodoDecision, map[string]any{"id": "todo_example_1", "title": "Ship the release"})
				// This event follows both to-do notifications on the same process stream, proving the subscriber has read them.
				h.extensions.emit("agent.finished", map[string]any{})
				waitLong(t, "sentinel event delivered", func() bool { return strings.Contains(view(), "value:1") })
				if v := view(); strings.Contains(v, "todo.") {
					t.Fatalf("a to-do event reached an extension that is not allowed it: %s", v)
				}
				return
			}
			// The sentinel behind them: everything the extension is going to be told has been read when it is counted.
			h.extensions.emit("agent.finished", map[string]any{})
			want := 3
			if tc.name == "reader-changed-only" {
				want = 2
			}
			waitLong(t, "to-do events delivered", func() bool { return strings.Contains(view(), fmt.Sprintf("value:%d", want)) })
			v := view()
			for _, w := range []string{`<todo.changed `, `"id":"todo_example_1"`, `"what":"updated"`, `"done":true`, `"title":"Ship the release"`, `"chat_id":"oc_example_1"`, `"thread_id":"omt_example_1"`, `"channel":"feishu"`} {
				if !strings.Contains(v, w) {
					t.Fatalf("todo.changed lacks %s: %s", w, v)
				}
			}
			if want == 3 {
				for _, w := range []string{`<todo.decision `, `"decision":{`, `"id":"dec_example_1"`, `"question":"Which branch"`, `"choice":"release-1.3"`, `"source":"thread"`} {
					if !strings.Contains(v, w) {
						t.Fatalf("todo.decision lacks %s: %s", w, v)
					}
				}
			} else if strings.Contains(v, "<todo.decision") {
				t.Fatalf("an extension subscribed to todo.changed only was told of a decision: %s", v)
			}
			// Switched off, or disabled, it is told nothing more.
			if err := h.extensions.reg.SetContribution(context.Background(), "example-ui", "events", "todos", false, "test"); err != nil {
				t.Fatal(err)
			}
			if ids := h.extensions.reg.EventSubscribers("todo.changed", extension.EventScopes["todo.changed"]); len(ids) != 0 {
				t.Fatalf("a switched-off contribution still receives to-do events: %v", ids)
			}
		})
	}
}

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

// waitEventQueuesEmpty waits until the workers have handed everything queued to the extensions.
func waitEventQueuesEmpty(t *testing.T, h *Manager) {
	t.Helper()
	for _, q := range []*extensionEvents{&h.extensions.events, &h.extensions.todoEvents} {
		waitLong(t, "the event queue to empty", func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return !q.running && len(q.pending) == 0
		})
	}
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
