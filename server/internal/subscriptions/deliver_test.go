package subscriptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Delivery to the Inbox (#2039): the things a subscription hands them, made up
// from made-up notices.

// note is a made-up notice as the fake tool lists it.
type note struct {
	id, thread, title, at, due, link string
}

func notesJSON(ns ...note) string {
	items := []map[string]any{}
	for _, n := range ns {
		m := map[string]any{"id": n.id, "title": n.title, "from": map[string]any{"name": "Office"}, "at": n.at, "text": "Made-up notice text"}
		for k, v := range map[string]string{"thread": n.thread, "due": n.due, "link": n.link} {
			if v != "" {
				m[k] = v
			}
		}
		items = append(items, m)
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b)
}

// threadRecipe reads every field, grouped by thread.
func threadRecipe() Recipe {
	return Recipe{Mode: "items", Tool: "list_notices", Args: json.RawMessage(`{"since":"{{since}}","limit":25}`), ItemsPath: "$.items",
		Fields: &connector.SubscriptionFields{ID: "$.id", Thread: "$.thread", Title: "$.title", Sender: "$.from.name", Time: "$.at",
			Body: "$.text", Link: "$.link", DueDate: "$.due"},
		Kind: "sub_notice", GroupBy: "thread"}
}

func (e *env) answer(s string) {
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return s, nil })
}

func (e *env) patch(id string, p Patch) Subscription {
	e.t.Helper()
	p.Revision = e.get(id).Revision
	out, err := e.svc.Update(e.ctx, id, p, Actor{ID: "device:phone"})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// inbox is subscription id as a source of an Inbox round.
func (e *env) inbox(id string) InboxSource {
	e.t.Helper()
	srcs, err := e.svc.InboxSources(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, s := range srcs {
		if s.ID == id {
			return s
		}
	}
	e.t.Fatalf("subscription %s is not an Inbox source", id)
	return InboxSource{}
}

// rows are the things that make or move rows (not the end of a notice).
func (e *env) rows(id string) []connector.InboxThing {
	var out []connector.InboxThing
	for _, t := range e.inbox(id).Things {
		if t.Done == "" {
			out = append(out, t)
		}
	}
	return out
}

func rowOf(t *testing.T, rows []connector.InboxThing, thread string) connector.InboxThing {
	t.Helper()
	for _, r := range rows {
		if r.Thread == thread {
			return r
		}
	}
	t.Fatalf("no row of thread %s in %+v", thread, rows)
	return connector.InboxThing{}
}

func ruleTier(t connector.InboxThing) string {
	if t.Extra == nil {
		return ""
	}
	return t.Extra.RuleTier
}

func TestInboxRowsCarryTheItemsAFallbackTierAndTheRules(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(
		note{id: "n1", thread: "t1", title: "Office hours moved", at: "2026-11-10T08:00:00Z", link: "https://example.com/n1"},
		note{id: "n2", thread: "t1", title: "Office hours moved again", at: "2026-11-10T08:30:00Z", link: "http://example.com/n2"},
		note{id: "n3", title: "Form due", at: "2026-11-10T07:00:00Z", due: "2026-11-12T18:00:00Z"},
	))
	sub := e.turnOn(e.create(threadRecipe()))
	if rows := e.rows(sub.ID); len(rows) != 0 {
		t.Fatalf("rows before the first run since the Inbox was turned on: %+v", rows)
	}
	e.svc.RunDue(e.ctx)
	if len(e.delivered) != 1 || e.delivered[0] != sub.ID {
		t.Fatalf("the destinations were told: %v", e.delivered)
	}
	src := e.inbox(sub.ID)
	if src.Name != "Notices" || !strings.Contains(src.Kinds["sub_digest"], "Notices") || src.Kinds[KindFailing] == "" {
		t.Fatalf("the source: %q %v", src.Name, src.Kinds)
	}
	rows := e.rows(sub.ID)
	if len(rows) != 2 {
		t.Fatalf("three caught-up items in two threads: %d rows", len(rows))
	}
	r := rowOf(t, rows, "sub:"+sub.ID+":t1")
	want := connector.InboxThing{Thread: "sub:" + sub.ID + ":t1", Kind: "sub_notice", Label: "Notices", Title: "Office hours moved again",
		Sender: "Office", Preview: "Made-up notice text", At: time.Date(2026, 11, 10, 8, 30, 0, 0, time.UTC), Last: "n2",
		Extra: &connector.ThingExtra{RuleTier: "fyi", Count: 2}, Tags: []connector.Tag{{Key: "source", Value: sub.ID, Name: "Notices"},
			{Key: connector.TagSender, Value: "office", Name: "Office"}}}
	ctx := r.Context
	r.Context = ""
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("the thread's row:\n got %+v\nwant %+v", r, want)
	}
	if !strings.Contains(ctx, "11-10 08:00 · Office · Office hours moved https://example.com/n1") ||
		!strings.Contains(ctx, "Office hours moved again\nOffice · 11-10 08:30\n\nMade-up notice text\n\nhttp://example.com/n2") {
		t.Fatalf("the thread's context:\n%s", ctx)
	}
	due := rowOf(t, rows, "sub:"+sub.ID+":n3")
	if ruleTier(due) != "action" || !strings.Contains(due.Context, "Due 11-12 18:00") || due.URL != "" || due.CanReply {
		t.Fatalf("an item with a due date: %+v", due)
	}
	// The fallback tier is the subscription's.
	for tier, want := range map[string][2]string{"wait": {"", ""}, "fyi": {"fyi", "fyi"}, "action": {"action", "action"}, "": {"fyi", "action"}} {
		e.patch(sub.ID, Patch{FallbackTier: ptr(tier)})
		rows := e.rows(sub.ID)
		if got := [2]string{ruleTier(rowOf(t, rows, "sub:"+sub.ID+":t1")), ruleTier(rowOf(t, rows, "sub:"+sub.ID+":n3"))}; got != want {
			t.Errorf("fallback %q: %v, want %v", tier, got, want)
		}
	}
	// Rules: the subscription's own, else its extension's, else none (the Inbox's own).
	if r := rowOf(t, e.rows(sub.ID), "sub:"+sub.ID+":n3"); r.Rules != "" {
		t.Fatalf("rules without any: %q", r.Rules)
	}
	e.extRules = "Notices from the office are FYI."
	if r := rowOf(t, e.rows(sub.ID), "sub:"+sub.ID+":n3"); r.Rules != e.extRules {
		t.Fatalf("the extension's rules: %q", r.Rules)
	}
	e.patch(sub.ID, Patch{TriageRules: ptr("  Forms need me.  ")})
	if r := rowOf(t, e.rows(sub.ID), "sub:"+sub.ID+":n3"); r.Rules != "Forms need me." {
		t.Fatalf("the subscription's rules: %q", r.Rules)
	}
}

func TestACatchUpOfThirtyIsOneDigestAndUpcomingDeadlinesStayRows(t *testing.T) {
	e := newEnv(t)
	var ns []note
	start := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	for i := range 30 {
		ns = append(ns, note{id: fmt.Sprint("n", i), title: fmt.Sprint("Notice ", i), at: start.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)})
	}
	ns[3].due = "2026-11-11T12:00:00Z" // still ahead: a row of its own
	ns[4].due = "2026-11-09T12:00:00Z" // past: in the digest
	e.answer(notesJSON(ns...))
	sub := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 2 {
		t.Fatalf("30 caught-up items: %d rows, want the digest and the upcoming deadline", len(rows))
	}
	digest := rowOf(t, rows, "sub:"+sub.ID+":catchup")
	if digest.Kind != "sub_digest" || digest.Title != "Notices: 29 items from the day before it was turned on" || ruleTier(digest) != "fyi" ||
		!digest.At.Equal(start) || !strings.HasPrefix(digest.Last, "digest:") || strings.Count(digest.Context, "\n") != 28 {
		t.Fatalf("the digest: %+v", digest)
	}
	if !strings.HasPrefix(digest.Context, "11-10 08:00 · Office · Notice 0\n") || !strings.Contains(digest.Context, "Notice 4") {
		t.Fatalf("the digest's lines, newest first, the past deadline among them:\n%s", digest.Context)
	}
	if r := rowOf(t, rows, "sub:"+sub.ID+":n3"); ruleTier(r) != "action" {
		t.Fatalf("the upcoming deadline: %+v", r)
	}
	// Later items come in as rows; the digest stays as it was.
	e.answer(notesJSON(append(ns, note{id: "n99", title: "A new notice", at: "2026-11-10T09:10:00Z"})...))
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	rows = e.rows(sub.ID)
	if len(rows) != 3 || rowOf(t, rows, "sub:"+sub.ID+":catchup").Last != digest.Last || rowOf(t, rows, "sub:"+sub.ID+":n99").Title != "A new notice" {
		t.Fatalf("after a later item: %+v", rows)
	}
	// "Wait for the model": no fallback, for the digest too.
	e.patch(sub.ID, Patch{FallbackTier: ptr("wait")})
	if d := rowOf(t, e.rows(sub.ID), "sub:"+sub.ID+":catchup"); d.Extra != nil {
		t.Fatalf("the digest waits for the model: %+v", d.Extra)
	}
}

func TestADigestListsFiftyAndCountsTheRest(t *testing.T) {
	e := newEnv(t)
	var ns []note
	for i := range 60 {
		ns = append(ns, note{id: fmt.Sprint("n", i), title: fmt.Sprint("Notice ", i), at: "2026-11-10T08:00:00Z"})
	}
	e.answer(notesJSON(ns...))
	sub := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	d := rowOf(t, e.rows(sub.ID), "sub:"+sub.ID+":catchup")
	lines := strings.Split(d.Context, "\n")
	if len(lines) != 51 || lines[50] != "and 10 more" || !strings.Contains(d.Title, "60 items") {
		t.Fatalf("a digest of 60: %d lines, the last %q; %q", len(lines), lines[len(lines)-1], d.Title)
	}
}

func TestASnapshotRowOnlyWhenItChanges(t *testing.T) {
	e := newEnv(t)
	e.answer("Balance: 20")
	r := Recipe{Mode: "snapshot", Tool: "balance", Args: json.RawMessage(`{}`), Kind: "sub_update", GroupBy: "source"}
	sub := e.turnOn(e.create(r))
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 1 || rows[0].Thread != "sub:"+sub.ID+":snapshot" || rows[0].Kind != "sub_update" || rows[0].Context != "Balance: 20" ||
		!strings.HasPrefix(rows[0].Last, "snap:") {
		t.Fatalf("the first snapshot: %+v", rows)
	}
	first := rows[0].Last
	e.clock.advance(24 * time.Hour)
	e.svc.RunDue(e.ctx)
	if rows := e.rows(sub.ID); len(rows) != 1 || rows[0].Last != first || len(e.store.Items()) != 1 {
		t.Fatalf("an unchanged snapshot: %+v, %d kept", rows, len(e.store.Items()))
	}
	e.answer("Balance: 12")
	e.clock.advance(24 * time.Hour)
	e.svc.RunDue(e.ctx)
	if rows := e.rows(sub.ID); len(rows) != 1 || rows[0].Last == first || rows[0].Context != "Balance: 12" {
		t.Fatalf("a changed snapshot: %+v", rows)
	}
}

// A snapshot is new when it differs from the last one, even when it returns to an earlier value (#13122).
func TestASnapshotBackToAnEarlierValueIsDelivered(t *testing.T) {
	e := newEnv(t)
	e.answer("Balance: 20")
	r := Recipe{Mode: "snapshot", Tool: "balance", Args: json.RawMessage(`{}`), Kind: "sub_update", GroupBy: "source"}
	sub := e.turnOn(e.create(r))
	e.svc.RunDue(e.ctx)
	e.answer("Balance: 12")
	e.clock.advance(24 * time.Hour)
	e.svc.RunDue(e.ctx)
	e.answer("Balance: 20")
	e.clock.advance(24 * time.Hour)
	e.svc.RunDue(e.ctx)
	got, err := e.store.GetSubscription(e.ctx, sub.ID)
	if err != nil || got == nil || len(got.Runs) == 0 {
		t.Fatalf("subscription: %+v, %v", got, err)
	}
	rows := e.rows(sub.ID)
	if len(e.store.Items()) != 3 || len(rows) != 1 || rows[0].Context != "Balance: 20" || got.Runs[len(got.Runs)-1].New != 1 {
		t.Fatalf("a snapshot back to an earlier value: %d kept, rows %+v, last run %+v", len(e.store.Items()), rows, got.Runs[len(got.Runs)-1])
	}
}

func TestARestartedManagerHandsTheDestinationsTheSameThings(t *testing.T) {
	e := newEnv(t)
	var ns []note
	for i := range 8 {
		ns = append(ns, note{id: fmt.Sprint("n", i), thread: fmt.Sprint("t", i%3), title: fmt.Sprint("Notice ", i), at: "2026-11-10T08:00:00Z"})
	}
	e.answer(notesJSON(ns...))
	sub := e.create(threadRecipe())
	e.patch(sub.ID, Patch{ToInbox: ptr(true)})
	e.svc.RunDue(e.ctx)
	things := e.inbox(sub.ID).Things
	e.svc = e.service()
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx) // the same answer again
	if again := e.inbox(sub.ID).Things; !reflect.DeepEqual(again, things) {
		t.Fatalf("after a restart and a replay the Inbox gets:\n%+v\nnot\n%+v", again, things)
	}
}

func TestADayOfFailuresIsOneFYINoticeDoneOnceItPullsAgain(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(note{id: "n1", title: "One", at: "2026-11-10T08:00:00Z"}))
	sub := e.turnOn(e.create(threadRecipe()))
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return "", errors.New("upstream said no") })
	notice := func() connector.InboxThing {
		for _, th := range e.inbox(sub.ID).Things {
			if th.Thread == "sub:"+sub.ID+":failing" {
				return th
			}
		}
		t.Fatal("no notice thing")
		return connector.InboxThing{}
	}
	e.svc.RunDue(e.ctx)
	if n := notice(); n.Done != "read" {
		t.Fatalf("a first failure: %+v", n)
	}
	for range 26 {
		e.clock.advance(time.Hour)
		e.svc.RunDue(e.ctx)
	}
	n := notice()
	if n.Done != "" || n.Tier != "fyi" || n.Kind != KindFailing || n.Title != "Notices hasn't pulled anything since 11-10 09:00" ||
		n.Preview != "The connector's call failed." || n.Last != fmt.Sprint("failing:", time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC).Unix()) ||
		n.Why == "" {
		t.Fatalf("a day of failures: %+v", n)
	}
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if again := notice(); again.Last != n.Last {
		t.Fatalf("one notice a failure: %q then %q", n.Last, again.Last)
	}
	e.answer(notesJSON(note{id: "n1", title: "One", at: "2026-11-10T08:00:00Z"}))
	for i := 0; i < 8 && e.get(sub.ID).State != StateOK; i++ {
		e.clock.advance(time.Hour)
		e.svc.RunDue(e.ctx)
	}
	if n := notice(); n.Done != "read" || n.Why != "The subscription pulls again" {
		t.Fatalf("after it pulls again: %+v", n)
	}
}

func TestDeletingWithItsDataHasTheDestinationsForgetIt(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	e.forget = func(Subscription) error { return errors.New("the assistant is away") }
	if err := e.svc.Delete(e.ctx, sub.ID, true, Actor{ID: "device:phone"}); err == nil {
		t.Fatal("a delete whose data could not be removed succeeded")
	}
	if got := e.get(sub.ID); got.State != StateOff || len(e.forgot) != 1 || e.forgot[0].ToInbox {
		t.Fatalf("after a failed removal: %s, forgot %d", got.State, len(e.forgot))
	}
	e.forget = nil
	if err := e.svc.Delete(e.ctx, sub.ID, true, Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Get(e.ctx, sub.ID); codeOf(err) != CodeNotFound || len(e.forgot) != 2 {
		t.Fatalf("after deleting with its data: %v, forgot %d", err, len(e.forgot))
	}
	other := e.turnOn(e.create(threadRecipe()))
	if err := e.svc.Delete(e.ctx, other.ID, false, Actor{ID: "device:phone"}); err != nil || len(e.forgot) != 2 {
		t.Fatalf("deleting without its data asked the destinations: %v %d", err, len(e.forgot))
	}
}

func TestHeldItemsAreKeptOnlyWhileTheInboxNeedsThem(t *testing.T) {
	e := newEnv(t)
	inbox := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	held := func(id string) int {
		n := 0
		for _, it := range e.store.Items() {
			if it.SubscriptionID == id {
				n++
			}
		}
		return n
	}
	if held(inbox.ID) != 3 {
		t.Fatalf("held: %d", held(inbox.ID))
	}
	e.clock.advance(9 * 24 * time.Hour)
	e.svc.RunDue(e.ctx)
	if held(inbox.ID) != 0 {
		t.Fatalf("after 9 days: %d (the Inbox's window is past)", held(inbox.ID))
	}
	if e.store.SeenCount(inbox.ID) != 3 {
		t.Fatal("pruning took the dedup keys")
	}
}

// The destinations' marks are kept in the database's document, and a run's commit keeps the catch-up mark only for
// the Inbox switch-on it saw.
func TestTheDestinationsMarksAreKept(t *testing.T) {
	from, mark := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 9, 5, 0, 0, time.UTC)
	sub := Subscription{ID: "sub_a", ToInbox: true, InboxFrom: &from, InboxCatchup: &mark}
	raw, err := encode(sub)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decode(raw)
	if err != nil || !sameTime(back.InboxFrom, &from) || !sameTime(back.InboxCatchup, &mark) {
		t.Fatalf("decoded: %+v %v", back, err)
	}
	// The Inbox was turned off and on again while a run went: its mark belongs to the earlier switch-on.
	again := from.Add(time.Hour)
	cur := Subscription{ID: "sub_a", ToInbox: true, InboxFrom: &again}
	applyRun(&cur, RunCommit{Sub: sub}, true)
	if cur.InboxCatchup != nil {
		t.Fatalf("a run kept the mark of another switch-on: %v", cur.InboxCatchup)
	}
	cur.InboxFrom = &from
	applyRun(&cur, RunCommit{Sub: sub}, false)
	if !sameTime(cur.InboxCatchup, &mark) {
		t.Fatal("a run's mark for the switch-on it saw was not kept")
	}
}

// A row of items carries its sender as a mute target (#5306), lowercased; a digest, a snapshot and the failure notice
// have no one sender.
func TestDeliveredSenderIsAMuteTarget(t *testing.T) {
	d := deliverer{sub: Subscription{ID: "sub_x", Label: "Notices"}, loc: time.UTC, lang: i18n.En}
	tags := d.tags(&Item{Sender: " Office "})
	if len(tags) != 2 || tags[1] != (connector.Tag{Key: connector.TagSender, Value: "office", Name: "Office"}) || tags[0].Key != "source" {
		t.Fatalf("tags: %+v", tags)
	}
	if tags := d.tags(&Item{}); len(tags) != 1 || tags[0].Value != "sub_x" {
		t.Fatalf("without a sender: %+v", tags)
	}
	if tags := d.tags(nil); len(tags) != 1 || tags[0].Value != "sub_x" {
		t.Fatalf("without an item: %+v", tags)
	}
	digest := d.digestThing([]Held{{Seq: 1, Item: Item{UID: "a", Title: "A", Sender: "Office"}}, {Seq: 2, Item: Item{UID: "b", Title: "B", Sender: "Shop"}}})
	if len(digest.Tags) != 1 {
		t.Errorf("a digest of several senders mutes none: %+v", digest.Tags)
	}
}

// A recipe's sender_id and chat make the row's mute targets match by id, as a built-in's do (#6165): the sender's id as
// the source writes it (not lowercased), with the display name to read; the chat's id with its name, or the id alone.
func TestDeliveredSenderAndChatByID(t *testing.T) {
	d := deliverer{sub: Subscription{ID: "sub_x", Label: "Team chat", Recipe: Recipe{GroupBy: "thread"}}, loc: time.UTC, lang: i18n.En}
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	th := d.threadThing("c1", []Held{
		{Seq: 1, Stored: at, Item: Item{UID: "m1", Title: "Earlier", Sender: "Ann", SenderID: "ou_Ann1", Chat: "oc_Team", ChatName: "Team"}},
		{Seq: 2, Stored: at.Add(time.Minute), Item: Item{UID: "m2", Title: "Newest", Sender: "Bo", SenderID: "ou_Bo2", Chat: "oc_Team", ChatName: "Team"}},
	})
	want := []connector.Tag{{Key: "source", Value: "sub_x", Name: "Team chat"},
		{Key: connector.TagSender, Value: "ou_Bo2", Name: "Bo"}, {Key: connector.TagChat, Value: "oc_Team", Name: "Team"}}
	if !slices.Equal(th.Tags, want) {
		t.Fatalf("tags of the newest item:\n got %+v\nwant %+v", th.Tags, want)
	}
	tags := d.tags(&Item{SenderID: "u1", Chat: "c9"})
	if tags[1] != (connector.Tag{Key: connector.TagSender, Value: "u1", Name: "u1"}) || tags[2] != (connector.Tag{Key: connector.TagChat, Value: "c9", Name: "c9"}) {
		t.Fatalf("ids without names: %+v", tags)
	}
}

// A recipe maps who and where (#6165) without making them content: adding sender_id, chat and chat_name to a recipe
// keeps every item's uid and content hash, so no item reads as changed; a path that finds nothing is warned about as
// for any field.
func TestRecipeMapsSenderAndChatIDsOutsideTheContent(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(`{"id":"m1","text":"Hi","from":{"name":" Bo ","id":" ou_Bo2 "},"chat":{"id":"oc_Team","name":"Team  room"}}`), &v); err != nil {
		t.Fatal(err)
	}
	plain := connector.SubscriptionFields{ID: "$.id", Title: "$.text", Sender: "$.from.name"}
	full := plain
	full.SenderID, full.Chat, full.ChatName = "$.from.id", "$.chat.id", "$.chat.name"
	a, b := mapItem(v, &plain, "sub_message", time.UTC), mapItem(v, &full, "sub_message", time.UTC)
	if b.item.SenderID != "ou_Bo2" || b.item.Chat != "oc_Team" || b.item.ChatName != "Team room" || b.item.Sender != "Bo" {
		t.Fatalf("mapped: %+v", b.item)
	}
	if a.hash != b.hash || a.item.UID != b.item.UID {
		t.Fatalf("who and where changed the content: %s %s / %s %s", a.hash, a.item.UID, b.hash, b.item.UID)
	}
	missing := full
	missing.Chat = "$.room"
	ws := emptyFieldWarnings(&missing, []mapped{mapItem(v, &missing, "sub_message", time.UTC)})
	if len(ws) != 1 || ws[0].Key != "fields.chat" {
		t.Fatalf("warnings: %+v", ws)
	}
	if ps := itemProblems(mapItem(v, &missing, "sub_message", time.UTC), &missing); len(ps) != 1 || ps[0].Key != "fields.chat" {
		t.Fatalf("problems: %+v", ps)
	}
}
