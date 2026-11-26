package assistant

import (
	"context"
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestInboxTaskOwnerBotHandoffPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, source, kind string
		want               bool
	}{
		{"owner request", "larkcli", "bot_handoff", false},
		{"ordinary human chat", "larkcli", KindDM, true},
		{"legacy human chat", "larkcli", KindBot, true},
		{"another connector's kind", "example", "bot_handoff", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := &InboxItem{Source: tc.source, Kind: tc.kind, Connector: taskNotification("ou_example")}
			if got := inboxTaskLinkEligible(it); got != tc.want {
				t.Fatalf("eligibility = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInboxTaskOwnerBotHandoffSkipsBothRouters(t *testing.T) {
	h, todo, it := taskLinkPolicyHub(t)
	const pr = "https://github.com/example/project/pull/123"
	h.todos.mu.Lock()
	h.todos.todos[todo.ID].PRURL = pr
	h.todos.mu.Unlock()
	it.Source, it.Kind, it.Connector = "larkcli", "bot_handoff", taskNotification("ou_example")
	it.Context = pr
	model, calls := captureModel(t, `{"task_id":"`+todo.ID+`","evidence":"`+todo.Title+`"}`)
	h.in.setModel(model)

	h.linkInboxTaskURLs(context.Background())
	if it.Agent.TodoID != "" {
		t.Fatalf("the URL router linked an owner request to %q", it.Agent.TodoID)
	}
	if h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: todo.ID, Title: todo.Title}}) ||
		it.Agent.TodoID != "" || len(calls()) != 0 {
		t.Fatalf("the owner request was judged or linked: %+v, calls=%d", it.Agent, len(calls()))
	}
}

func TestInboxTaskOwnerBotHandoffStaleLinkDoesNotSignal(t *testing.T) {
	s, f := newQuietService(t)
	turnInboxSignalsOn(t, s)
	todo := supervisedTodo(t, s, "Restore the terminal entry")
	it := linkedItem(s, "larkcli:owner-request", todo.ID, "message", "Check account limits", 1)
	it.Source, it.Kind, it.Connector = "larkcli", "bot_handoff", taskNotification("ou_example")

	s.superviseInbox(context.Background(), []*InboxItem{it}, map[*InboxItem]bool{it: true})
	if signal, ok := pendingInbox(s, todo.ID); ok {
		t.Fatalf("the owner's request raised a third-party task signal: %+v", signal)
	}
	if it.Agent.signaled != 0 || it.Agent.signaledOf != "" || len(f.notices()) != 0 {
		t.Fatal("the owner request was counted or announced as task activity")
	}
}

func TestInboxTaskOwnerBotHandoffDoesNotFollowContinuation(t *testing.T) {
	s, _ := continuesService(t)
	previous := supervisedTodo(t, s, "Old work")
	next := supervisedTodo(t, s, "Continuing work")
	closeTodo(t, s, previous.ID)
	continuesTask(s, next.ID, previous.ID)
	it := heldItem(s, previous.ID)
	s.inbox.mu.Lock()
	it.Source, it.Kind, it.Connector = "larkcli", "bot_handoff", taskNotification("ou_example")
	s.inbox.mu.Unlock()

	s.superviseInbox(context.Background(), []*InboxItem{it}, nil)
	if got := linkOf(s, it); got != previous.ID {
		t.Fatalf("an excluded owner request followed the task to %q", got)
	}
	if signal, ok := pendingInbox(s, next.ID); ok {
		t.Fatalf("the continuing task received an owner request: %+v", signal)
	}
}
