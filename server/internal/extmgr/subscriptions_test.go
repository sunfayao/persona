package extmgr

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// A subscription is an internal source (#2039): every Inbox round names it and, unless it reads only the Feishu chats,
// hands in its things as a plugin's (triaged first). Deleting it with its data asks the assistant to remove the
// Inbox's, and while the assistant is away the subscription stays, switched off, for another try.
func TestSubscriptionsAreInboxSources(t *testing.T) {
	ctx := context.Background()
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	store := subscriptions.NewMemStore()
	now := time.Now().UTC()
	from, mark := now.Add(-time.Minute), now.Add(-30*time.Second)
	sub := subscriptions.Subscription{ID: "sub_example", Connector: "example", Source: "notices", Label: "Notices",
		Recipe: subscriptions.Recipe{Mode: "items", Kind: "sub_notice", GroupBy: "source"}, ToInbox: true,
		InboxFrom: &from, InboxCatchup: &mark, InboxGroup: "source", Revision: 1, CreatedAt: now.Add(-time.Hour)}
	if err := store.CreateSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-2 * time.Minute)
	items := []subscriptions.Item{{UID: "n1", Title: "Office hours moved", Time: &at, Body: "Made-up text", Kind: "sub_notice"},
		{UID: "n2", Title: "Form due", Time: &at, Body: "Made-up text", Kind: "sub_notice"}}
	if _, err := store.CommitRun(ctx, subscriptions.RunCommit{Sub: sub, Revision: 1, Items: items, Now: mark}); err != nil {
		t.Fatal(err)
	}
	h.subs = subscriptions.New(h.subscriptionOptions(store))

	r := h.readInbox(ctx, roundPoll)
	var src *assistantwire.InboxSource
	for i := range r.Sources {
		if r.Sources[i].ID == sub.ID {
			src = &r.Sources[i]
		}
	}
	// One row for the subscription grouped by source (#5671) and the failure notice's end; its count is the Inbox's to read.
	if r.Names[sub.ID] != "Notices" || src == nil || !src.Extension || !src.Counts || src.Kinds["sub_digest"] == "" || len(src.Things) != 2 {
		t.Fatalf("the round: names %v, source %+v", r.Names, src)
	}
	// What it reads, for the AI twin (#6222).
	if src.Via != "example" || src.ViaSource != "notices" {
		t.Fatalf("what the subscription reads: %q %q", src.Via, src.ViaSource)
	}
	if chats := h.readInbox(ctx, roundChats); chats.Names[sub.ID] != "Notices" || len(chats.Sources) != 0 {
		t.Fatalf("a round of the Feishu chats: %+v", chats)
	}
	// A run that failed: the next round says the subscription is behind, as a built-in's failed read does, and still
	// hands its rows (#6189).
	cur, _ := store.GetSubscription(ctx, sub.ID)
	failing := *cur
	failing.RunState = subscriptions.StateFailing
	if ok, err := store.UpdateSubscription(ctx, failing, cur.Revision); !ok || err != nil {
		t.Fatalf("update: %v %v", ok, err)
	}
	found := false
	for _, s := range h.readInbox(ctx, roundPoll).Sources {
		if s.ID == sub.ID {
			found = true
			if s.ReadError != assistantwire.ReadFailed || len(s.Things) != 2 {
				t.Fatalf("a failing subscription's source: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("the failing subscription left the round")
	}

	var failure *subscriptions.Error
	if err := h.subs.Delete(ctx, sub.ID, true, subscriptions.Actor{}); !errors.As(err, &failure) || failure.Status != http.StatusServiceUnavailable || failure.Code != assistantwire.CodeAssistantOffline {
		t.Fatalf("deleting its data with the assistant away: %v", err)
	}
	if left, _ := store.GetSubscription(ctx, sub.ID); left == nil || left.ToInbox {
		t.Fatalf("after the failed removal: %+v", left)
	}
}
