package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A to-do's writes under an op id say how they stand on the socket (todo_send_state, todo_receipts.go, #10465):
// processing as soon as the hub has them, then how the relay ended; only to the full apps that asked.

// todoRoutePath is the conversation route a to-do's message goes on; todoRouteBody names to-do todo_a1 in it.
const (
	todoRoutePath = "/api/v1/assistant/conversations/conv1/route"
	todoRouteBody = `{"todo_id":"todo_a1","text":"Book the earlier train"}`
)

// todoSendApps are the apps a to-do write's states may reach: knows asked for them on its socket's URL, hello in its
// client_hello; the watch (an answer-only device) asked too; other did not ask.
type todoSendApps struct{ knows, hello, watch, other *client }

func newTodoSendApps(t *testing.T, h *Hub) todoSendApps {
	t.Helper()
	a := todoSendApps{
		knows: &client{out: make(chan []byte, 64), role: roleFull, urlFeatures: map[string]bool{proto.FeatureTodoSendState: true}},
		hello: &client{out: make(chan []byte, 64), role: roleFull},
		watch: &client{out: make(chan []byte, 64), role: roleRetiredAnswer, urlFeatures: map[string]bool{proto.FeatureTodoSendState: true}},
		other: &client{out: make(chan []byte, 64), role: roleFull},
	}
	addTestClient(h, a.knows, a.hello, a.watch, a.other)
	h.onClientMessage(context.Background(), a.hello, []byte(`{"type":"client_hello","features":["todo_send_state"]}`))
	if got := appGot(a.hello, 200*time.Millisecond); !strings.Contains(got, "client_hello_ack") {
		t.Fatalf("no client_hello_ack: %s", got)
	}
	return a
}

// nextTodoSendState is the next todo_send_state c gets, within five seconds.
func nextTodoSendState(t *testing.T, c *client) proto.TodoSendState {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-c.out:
			var event proto.TodoSendState
			if json.Unmarshal(raw, &event) == nil && event.Type == "todo_send_state" {
				return event
			}
		case <-timer.C:
			t.Fatal("no todo_send_state within five seconds")
		}
	}
}

// noTodoSendState: c gets no todo_send_state for a moment.
func noTodoSendState(t *testing.T, c *client, who string) {
	t.Helper()
	if got := appGot(c, 200*time.Millisecond); strings.Contains(got, `"todo_send_state"`) {
		t.Fatalf("%s got a to-do's send state: %s", who, got)
	}
}

// The conversation route keeps a todo_route receipt; one that reached no manager is asked again under its op id. How
// a relay's answer reads as a final state, and as the receipt's: failed only when it shows nothing was taken.
func TestTodoRouteReceiptKindAndOutcome(t *testing.T) {
	offline, _ := json.Marshal(map[string]string{"code": assistantwire.CodeAssistantOffline})
	doubt, _ := json.Marshal(map[string]string{"code": assistantwire.CodeAssistantInDoubt})
	if !clientOpRetry(todoRouteOp, offline) || clientOpRetry(todoRouteOp, doubt) || clientOpRetry(todoRouteOp, []byte(`{"code":"conflict"}`)) {
		t.Fatal("only an offline manager's refusal may be asked again under the same op id")
	}
	const route = "POST /api/v1/assistant/conversations/{id}/route"
	if kind := todoReceiptRoutes[route]; kind != todoRouteOp || !slices.Contains(assistantwire.Routes, route) {
		t.Fatalf("the conversation route's receipt kind %q", kind)
	}
	for _, tc := range []struct {
		status        int
		body          string
		want, receipt string
	}{
		{200, `{"item":{"id":"f1"}}`, proto.MessageSendDelivered, "done"},
		{0, ``, proto.MessageSendDelivered, "done"},
		{200, `{"outcome":"answered","delivered":"none"}`, proto.MessageSendDelivered, "done"},
		{502, `{"code":"assistant_in_doubt","delivered":"unknown"}`, proto.MessageSendUnknown, "unknown"},
		{409, `{"code":"conflict"}`, proto.MessageSendUnknown, "unknown"}, // the conversation route's answer to any error
		{502, `{"code":"accept_not_taken"}`, proto.MessageSendUnknown, "unknown"},
		{502, `{"error":"bus: closed"}`, proto.MessageSendUnknown, "unknown"},
		{409, `{"code":"ask_stale"}`, proto.MessageSendUnknown, "unknown"},
		{409, `{"code":"input_draft","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{409, `{"code":"conflict","delivered":"yes"}`, proto.MessageSendUnknown, "unknown"},
		{423, `{"code":"dialog_open","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{503, `{"code":"assistant_offline"}`, proto.MessageSendFailed, "failed"},
		{404, `{"code":"no_manager"}`, proto.MessageSendFailed, "failed"},
		{400, `{"code":"bad_request"}`, proto.MessageSendFailed, "failed"},
		{403, `{"code":"forbidden","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{400, `{"code":"bad_request","delivered":"unknown"}`, proto.MessageSendUnknown, "unknown"},
		// A newer manager says delivered on every refusal of a to-do's write (spec/fixtures/todo-writes).
		{409, `{"code":"` + assistantwire.CodeSendInDoubt + `","delivered":"unknown","retryable":false}`, proto.MessageSendUnknown, "unknown"},
		{502, `{"code":"` + assistantwire.CodeSendInDoubt + `","delivered":"yes","retryable":false}`, proto.MessageSendUnknown, "unknown"},
		{404, `{"code":"todo_not_found","delivered":"yes"}`, proto.MessageSendUnknown, "unknown"},
		{409, `{"code":"conflict","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{409, `{"code":"ask_stale","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{502, `{"code":"accept_not_taken","delivered":"none"}`, proto.MessageSendFailed, "failed"},
		{503, `{"code":"internal","delivered":"none"}`, proto.MessageSendFailed, "failed"},
	} {
		if got := todoReceiptState(tc.status, []byte(tc.body)); got != tc.receipt {
			t.Errorf("receipt %d %s: %s, want %s", tc.status, tc.body, got, tc.receipt)
		}
		if got := todoSendOutcome(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("%d %s: %s, want %s", tc.status, tc.body, got, tc.want)
		}
	}
}

// A to-do write under an op id is relayed with todoRelayWait, past the manager's own longest wait, not the plain
// relay's five minutes; without an op id the request goes on as it came.
func TestTodoRelayWaitsPastTheManager(t *testing.T) {
	if managerLongest := 6*time.Minute + 90*time.Second; todoRelayWait <= managerLongest { // a new Agent's start, then its send
		t.Fatalf("todoRelayWait %s is not past the manager's %s", todoRelayWait, managerLongest)
	}
	h := newTestHub(t)
	h.store = &receiptStore{memStore: newMemStore(), ops: &clientOpsStore{ops: map[string]db.ClientOp{}}}
	var left time.Duration
	var bounded bool
	relay := h.withTodoReceipt(todoRouteOp, func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		left, bounded = time.Until(deadline), ok
		w.WriteHeader(http.StatusOK)
	})
	for _, op := range []string{"00000000-0000-4000-8000-000000140465", ""} {
		r := httptest.NewRequest(http.MethodPost, todoRoutePath, strings.NewReader(todoRouteBody))
		if op != "" {
			r.Header.Set("X-Persona-Op-Id", op)
		}
		relay(httptest.NewRecorder(), r)
		if op != "" && (!bounded || left <= todoRelayWait-time.Minute) {
			t.Fatalf("under an op id: deadline %t in %s", bounded, left)
		}
		if op == "" && bounded {
			t.Fatalf("without an op id: a deadline in %s", left)
		}
	}
}

// spec/fixtures/todo-writes, the hub's half: each answer a manager gives a to-do's write that carries the owner's
// words, which the manager answers for each case (assistant/todo_write_outcome_test.go), relayed to an app that sent
// the write under an op id. The final todo_send_state and the receipt are what the case names: failed only for an
// answer that says nothing was typed, unknown for words that may be with the Agent or are, delivered for a 2xx; asked
// again, the same reply and no second state.
func TestTodoWriteOutcomeFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "fixtures", "todo-writes", "outcomes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int
		Cases   []struct {
			Name  string
			Input struct{ Route string }
			// Expected: the manager's answer (status, code, delivered), and what the hub makes of it.
			Expected struct {
				Status                          int
				Code, Delivered, State, Receipt string
			}
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != 1 || len(doc.Cases) == 0 {
		t.Fatalf("spec/fixtures/todo-writes/outcomes.json: version %d, %d cases, %v", doc.Version, len(doc.Cases), err)
	}
	h := newTestHub(t)
	store := &receiptStore{memStore: newMemStore(), ops: &clientOpsStore{ops: map[string]db.ClientOp{}}}
	h.store = store
	apps := newTodoSendApps(t, h)
	manager, _ := connectInboxManager(t, h)
	answer := assistantwire.HTTPResponse{}
	manager.mu.Lock()
	manager.http = func(context.Context, assistantwire.HTTPRequest) (any, error) {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return answer, nil
	}
	manager.mu.Unlock()
	mux := h.routes()
	for i, c := range doc.Cases {
		want := c.Expected
		body := map[string]any{"error": "the manager's words"}
		path, sent := todoRoutePath, todoRouteBody
		switch {
		case want.Status/100 == 2:
			body = map[string]any{"item": map[string]string{"id": "f1"}}
		}
		for key, value := range map[string]string{"code": want.Code, "delivered": want.Delivered} {
			if value != "" {
				body[key] = value
			}
		}
		reply, _ := json.Marshal(body)
		manager.mu.Lock()
		answer = assistantwire.HTTPResponse{Status: want.Status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: reply}
		manager.mu.Unlock()
		op := fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x10465300+i)
		for range 2 {
			if code, out := postTodo(t, context.Background(), h, mux, path, op, sent); code != want.Status || out != string(reply) {
				t.Fatalf("%s: %d %s, want %d %s", c.Name, code, out, want.Status, reply)
			}
		}
		if event := nextTodoSendState(t, apps.knows); event.State != proto.MessageSendProcessing || event.ClientOpID != op {
			t.Fatalf("%s: processing %+v", c.Name, event)
		}
		if event := nextTodoSendState(t, apps.knows); event.State != want.State || event.ClientOpID != op {
			t.Fatalf("%s: %+v, want %s", c.Name, event, want.State)
		}
		if got := receiptStates(store)[op]; got != want.Receipt {
			t.Fatalf("%s: receipt %q, want %q", c.Name, got, want.Receipt)
		}
		noTodoSendState(t, apps.knows, c.Name+" asked again")
	}
}
