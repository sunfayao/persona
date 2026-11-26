package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
)

func taskNotification(sender string) *InboxConnector {
	return &InboxConnector{Thread: "larkcli:chat:example", Tags: []connector.Tag{{Key: "sender", Value: sender}}}
}

func TestInboxTaskBulkNotificationsSkipBothRouters(t *testing.T) {
	for _, title := range []string{"Build result", "Daily menu"} {
		t.Run(title, func(t *testing.T) {
			h, todo, it := taskLinkPolicyHub(t)
			const pr = "https://github.com/example/project/pull/123"
			h.todos.mu.Lock()
			h.todos.todos[todo.ID].PRURL = pr
			h.todos.mu.Unlock()
			it.Kind, it.Title, it.Context, it.Connector = KindBot, title, pr, taskNotification("cli_example")
			model, calls := captureModel(t, `{"task_id":"`+todo.ID+`"}`)
			h.in.setModel(model)
			h.linkInboxTaskURLs(context.Background())
			if h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: todo.ID}}) || it.Agent.TodoID != "" || len(calls()) != 0 {
				t.Fatalf("a broadcast was associated or judged: %+v, calls=%d", it.Agent, len(calls()))
			}
		})
	}
}

func TestInboxTaskLegacyHumanCanStillLink(t *testing.T) {
	h, todo, it := taskLinkPolicyHub(t)
	it.Kind, it.Connector = KindBot, taskNotification("ou_example")
	it.Context = "I forwarded a build notification. Please investigate the failure."
	model, calls := captureModel(t, string(rawJSON(map[string]string{"task_id": todo.ID, "evidence": todo.Title})))
	h.in.setModel(model)
	if !h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: todo.ID, Title: todo.Title}}) || it.Agent.TodoID != todo.ID || len(calls()) != 1 {
		t.Fatalf("human conversation excluded: %+v", it.Agent)
	}
	// An explicit user identity in another connector's opaque ID is not a Lark identity.
	it.Connector.Thread = "example:chat:other"
	if inboxTaskLinkEligible(it) {
		t.Fatal("a different connector's sender ID overrode its bot kind")
	}
}

func TestInboxTaskHandledBroadcastDoesNotFollowContinuation(t *testing.T) {
	s, _ := continuesService(t)
	previous := supervisedTodo(t, s, "Old work")
	next := supervisedTodo(t, s, "Continuing work")
	closeTodo(t, s, previous.ID)
	continuesTask(s, next.ID, previous.ID)
	it := heldItem(s, previous.ID)
	now := time.Now()
	s.inbox.mu.Lock()
	it.Kind, it.Connector, it.DoneAt = KindBot, taskNotification("cli_example"), &now
	s.inbox.mu.Unlock()

	s.linkInboxTaskURLs(context.Background())
	if got := linkOf(s, it); got != previous.ID {
		t.Fatalf("a handled broadcast moved onto active work: %q, want legacy link %q", got, previous.ID)
	}
	if signal, ok := pendingInbox(s, next.ID); ok {
		t.Fatalf("a handled broadcast raised a signal for the continuing task: %+v", signal)
	}
}

func taskLinkPolicyHub(t *testing.T) (*inboxHub, Todo, *InboxItem) {
	t.Helper()
	s, f := newQuietService(t)
	s.inbox.handing.Lock() // isolate the routing policy from background processing
	t.Cleanup(s.inbox.handing.Unlock)
	in := newFakeInbox()
	f.inbox, in.svc = in, s
	h := &inboxHub{Service: s, f: f, in: in}
	if err := s.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.setConfig(func(c *assistantwire.InboxConfig) { c.Agent.Enabled = true })
	todo := startedTask(t, s, f)
	it := actionItem("example:old", time.Now().Add(-72*time.Hour))
	it.Agent = &InboxAgent{State: AgentDrafted, MachineID: "box", SessionID: "processing", Of: it.Last,
		Draft: "The fix is being checked.", Note: "Keep this draft."}
	it.Handling = &InboxHandling{State: HandlingDone}
	it.Recap = "The investigation traced the crash to screen rotation."
	it.Context = "The original human message reports a login crash when rotating the screen."
	it.Suggestion = &InboxSuggestion{Kind: SuggestReply, Text: it.Agent.Draft, Version: 3}
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	return h, todo, it
}
