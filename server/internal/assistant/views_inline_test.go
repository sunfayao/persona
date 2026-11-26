package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/repositorystore"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestInlineViewsUseOnlyTrustedAssistantItems(t *testing.T) {
	connection := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, connection, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	f := newFakeHub().withMachine()
	conversation := "inline-" + randomHex(8)
	f.setSessions("server", proto.Session{SessionID: conversation, Assistant: true}, proto.Session{SessionID: "ordinary", Assistant: false})
	s := newServiceOn(t, f, database)
	card := `{"fallback":"Choose","sections":[{"items":[{"type":"button","title":"Yes","action":{"id":"yes","reply":"Trusted inline reply"}}]}]}`
	text := "```persona-view\ninvalid\n```\n~~~persona-view\n" + card + "\n~~~\n"
	items := []proto.Item{
		{ID: "user#0", Role: "user", Kind: proto.KindText, Text: text},
		{ID: "tool#0", Role: "assistant", Kind: proto.KindToolResult, Text: text},
		{ID: "example#0", Role: "assistant", Kind: proto.KindText, Text: "````markdown\n```persona-view\n" + card + "\n```\n````"},
		{ID: "partial#0", Role: "assistant", Kind: proto.KindText, Text: "```persona-view\n" + card},
		{ID: "truncated#0", Role: "assistant", Kind: proto.KindText, Text: text, FullSize: 100000},
		{ID: "trusted#0", Role: "assistant", Kind: proto.KindText, Text: text},
	}
	s.SessionItems(ctx, SessionItems{MachineID: "server", SessionID: conversation, Items: items})
	keys, err := database.AssistantViewBlockKeys(ctx, conversation)
	if err != nil || len(keys) != 1 || keys[0] != "trusted#0/1" {
		t.Fatalf("keys %v %v", keys, err)
	}
	if strings.Count(f.eventTypes(), "assistant_view_block") != 1 {
		t.Fatalf("registration events %q", f.eventTypes())
	}
	s.SessionItems(ctx, SessionItems{MachineID: "server", SessionID: conversation, Items: items})
	if strings.Count(f.eventTypes(), "assistant_view_block") != 1 {
		t.Fatalf("replay broadcast another registration: %q", f.eventTypes())
	}
	s.SessionItems(ctx, SessionItems{MachineID: "server", SessionID: "ordinary", Items: items})
	keys, err = database.AssistantViewBlockKeys(ctx, "ordinary")
	if err != nil || len(keys) != 0 {
		t.Fatalf("ordinary keys %v %v", keys, err)
	}
	path := "/api/v1/assistant/conversations/" + conversation + "/views/" + url.PathEscape("trusted#0/1") + "/actions"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"action":"yes","client_msg_id":"client"}`)))
	if w.Code != 200 {
		t.Fatalf("escaped key submit %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		a, err := database.AssistantViewAction(ctx, conversation, "trusted#0/1")
		if err != nil {
			t.Fatal(err)
		}
		if a != nil && a.Status != "pending" {
			if !strings.HasPrefix(a.Text, "Trusted inline reply") {
				t.Fatalf("message %q", a.Text)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("inline action delivery did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Source errors are diagnostics, not another executable view; valid content retains its block index.
	feedback, err := database.PeekAssistantViewErrors(ctx, conversation, "server", 5)
	if err != nil || len(feedback) < 1 {
		t.Fatalf("feedback %v %v", feedback, err)
	}
	var action map[string]any
	if json.Unmarshal(w.Body.Bytes(), &action) != nil || action["action"] == nil {
		t.Fatal("no action result")
	}
}

type inlineFullHub struct {
	*fakeHub
	full proto.Item
}

func (h inlineFullHub) DeviceCall(ctx context.Context, p assistantwire.DeviceCall) (json.RawMessage, error) {
	if p.Method == "session.item" {
		return json.Marshal(h.full)
	}
	return h.fakeHub.DeviceCall(ctx, p)
}

func TestInlineTurnRegistersWithoutAnAppAndReadsFullText(t *testing.T) {
	connection := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, connection, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	f := newFakeHub().withMachine()
	conversation := "unwatched-" + randomHex(8)
	f.setSessions("server", proto.Session{SessionID: conversation, Assistant: true}, proto.Session{SessionID: "ordinary"})
	f.setItems(proto.Item{ID: "long#0", Role: "assistant", Kind: proto.KindText, Text: "A long conclusion before the card", FullSize: 100000})
	h := inlineFullHub{fakeHub: f, full: proto.Item{ID: "long#0", Role: "assistant", Kind: proto.KindText, Text: "Conclusion\n```persona-view\n{\"fallback\":\"Finished\",\"sections\":[]}\n```\n"}}
	s := withSettings(t, f, New(Options{Hub: h, Store: func() Store { return database }, Logf: testLogf(t)}))
	ref := SessionRef{MachineID: "server", SessionID: conversation}
	s.registerInlineTurn(ctx, SessionRef{MachineID: "server", SessionID: "ordinary"})
	keys, err := database.AssistantViewBlockKeys(ctx, "ordinary")
	if err != nil || len(keys) != 0 {
		t.Fatalf("ordinary turn keys %v %v", keys, err)
	}
	s.registerInlineTurn(ctx, ref)
	keys, err = database.AssistantViewBlockKeys(ctx, conversation)
	if err != nil || len(keys) != 1 || keys[0] != "long#0/0" {
		t.Fatalf("full reply keys %v %v", keys, err)
	}
}

type memInlineViewStore struct {
	*memStore
	vmu    sync.Mutex
	blocks map[string]map[string]db.AssistantViewBlockRow
}

var (
	_ Store           = (*memInlineViewStore)(nil)
	_ inlineViewStore = (*memInlineViewStore)(nil)
)

func newMemInlineViewStore() *memInlineViewStore {
	return &memInlineViewStore{
		memStore: &memStore{},
		blocks:   map[string]map[string]db.AssistantViewBlockRow{},
	}
}

func (m *memInlineViewStore) RegisterAssistantViewBlock(_ context.Context, row db.AssistantViewBlockRow) (bool, error) {
	m.vmu.Lock()
	defer m.vmu.Unlock()
	conv := m.blocks[row.Conversation]
	if conv == nil {
		conv = map[string]db.AssistantViewBlockRow{}
		m.blocks[row.Conversation] = conv
	}
	if _, exists := conv[row.Key]; exists {
		return false, nil
	}
	conv[row.Key] = row
	return true, nil
}

func (m *memInlineViewStore) AssistantViewBlockKeys(_ context.Context, conversation string) ([]string, error) {
	m.vmu.Lock()
	defer m.vmu.Unlock()
	conv := m.blocks[conversation]
	out := make([]string, 0, len(conv))
	for k := range conv {
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

func (m *memInlineViewStore) RecordAssistantViewErrors(_ context.Context, _, _, _ string, _ []string, _ time.Time) error {
	return nil
}

func TestInlineTurnRegistersFinalIDAfterSettleAndIsIdempotent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeHub().withMachine()
		f.ownModels = true
		f.settings = &Settings{Timezone: "Asia/Shanghai"}
		st := newMemInlineViewStore()
		s := newServiceOn(t, f, st)
		t.Cleanup(func() {
			if q := s.fleet.q.ch; q != nil {
				close(q)
			}
		})
		const delay = time.Minute
		f.mu.Lock()
		f.hello.DoneDelayMS = delay.Milliseconds()
		f.mu.Unlock()

		ctx := context.Background()
		conversation := "agy-settle-test"
		f.setSessions("server", proto.Session{SessionID: conversation, Assistant: true, Status: proto.StatusDone})
		card := `{"fallback":"Choose","sections":[{"items":[{"type":"button","title":"Yes","action":{"id":"yes","reply":"Choose yes"}}]}]}`
		text := "Options:\n```persona-view\n" + card + "\n```\n"

		// At TurnEnded, agy has only the temporary unexported item id (before native transcript flush).
		f.setItems(proto.Item{ID: "1785#0", Seq: 1785, Role: "assistant", Kind: proto.KindText, Text: text})
		s.TurnEnded(ctx, SessionRef{MachineID: "server", SessionID: conversation})
		synctest.Wait()

		keys, err := st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0"}) {
			t.Fatalf("after TurnEnded: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 1 {
			t.Fatalf("after TurnEnded: assistant_view_block count=%d, want 1 (%q)", got, f.eventTypes())
		}

		// Native transcript flushes with the final persisted item id before settleDelay elapses.
		f.setItems(proto.Item{ID: "1790@abcd#1", Seq: 1790, Role: "assistant", Kind: proto.KindText, Text: text})
		elapse(delay - time.Millisecond)
		keys, err = st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0"}) {
			t.Fatalf("before settleDelay: keys=%v err=%v", keys, err)
		}

		// Once settleDelay elapses, afterSettle registers the final persisted item id and broadcasts it once.
		elapse(time.Millisecond)
		keys, err = st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0", "1790@abcd#1/0"}) {
			t.Fatalf("after settleDelay: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 2 {
			t.Fatalf("after settleDelay: assistant_view_block count=%d, want 2 (%q)", got, f.eventTypes())
		}

		// Re-running registration for the same final item id is idempotent and broadcasts no duplicate event.
		s.registerInlineTurnSettled(ctx, "server", conversation)
		keys, err = st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0", "1790@abcd#1/0"}) {
			t.Fatalf("after idempotent replay: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 2 {
			t.Fatalf("after idempotent replay: assistant_view_block count=%d, want 2 (%q)", got, f.eventTypes())
		}
	})
}

func TestInlineTurnRegistersWhenStoppedAssistantRevAdvances(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeHub()
		f.ownModels = true
		f.settings = &Settings{Timezone: "Asia/Shanghai"}
		d := newFleetDevs()
		d.add("mac", "Mac")
		f.setSessions("mac") // transcript reads use the same machine the fleet lists
		st := newMemInlineViewStore()
		s := New(Options{Hub: f, Store: func() Store { return st }, Logf: testLogf(t), Devices: d})
		ft := &fleetTest{t: t, s: s, d: d, f: f}
		t.Cleanup(func() {
			if q := s.fleet.q.ch; q != nil {
				close(q)
			}
		})
		const delay = time.Minute
		f.mu.Lock()
		f.hello.DoneDelayMS = delay.Milliseconds()
		f.hello.Notify = assistantwire.NotifyTimings{DoneDelayMS: delay.Milliseconds(), LongTurnMS: 60_000}
		f.mu.Unlock()

		ctx := context.Background()
		f.setSessions("mac") // the node's transcript reads go through the hub
		machine, ok := s.fleetMachine("mac")
		if !ok {
			t.Fatal("missing fleet machine mac")
		}
		conversation := "agy-rev-advance"
		card := `{"fallback":"Choose","sections":[{"items":[{"type":"button","title":"Yes","action":{"id":"yes","reply":"Choose yes"}}]}]}`
		text := "Options:\n```persona-view\n" + card + "\n```\n"

		ft.sessions("mac", proto.Session{
			SessionID: conversation,
			Agent:     "agy",
			Cwd:       machine.assistantCwd(),
			Status:    proto.StatusWorking,
			Rev:       1780,
			UpdatedAt: time.Now(),
		})
		synctest.Wait()

		// Turn ends before native transcript export; only the temporary item id is available.
		f.setItems(proto.Item{ID: "1785#0", Seq: 1785, Role: "assistant", Kind: proto.KindText, Text: text})
		ft.sessions("mac", proto.Session{
			SessionID: conversation,
			Agent:     "agy",
			Cwd:       machine.assistantCwd(),
			Status:    proto.StatusDone,
			Rev:       1785,
			LastText:  "Options",
			UpdatedAt: time.Now(),
		})
		synctest.Wait()

		keys, err := st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0"}) {
			t.Fatalf("after TurnEnded: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 1 {
			t.Fatalf("after TurnEnded: assistant_view_block count=%d, want 1 (%q)", got, f.eventTypes())
		}

		// Native transcript flushes while still within the settle window, advancing rev to 1790.
		f.setItems(proto.Item{ID: "1790@abcd#1", Seq: 1790, Role: "assistant", Kind: proto.KindText, Text: text})
		ft.sessions("mac", proto.Session{
			SessionID: conversation,
			Agent:     "agy",
			Cwd:       machine.assistantCwd(),
			Status:    proto.StatusDone,
			Rev:       1790,
			LastText:  "Options",
			UpdatedAt: time.Now(),
		})
		synctest.Wait()

		keys, err = st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0", "1790@abcd#1/0"}) {
			t.Fatalf("after rev advance: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 2 {
			t.Fatalf("after rev advance: assistant_view_block count=%d, want 2 (%q)", got, f.eventTypes())
		}

		// Once afterSettle fires later, re-registration is idempotent and emits no duplicate event.
		elapse(delay)
		keys, err = st.AssistantViewBlockKeys(ctx, conversation)
		if err != nil || !slices.Equal(keys, []string{"1785#0/0", "1790@abcd#1/0"}) {
			t.Fatalf("after settleDelay: keys=%v err=%v", keys, err)
		}
		if got := strings.Count(f.eventTypes(), "assistant_view_block"); got != 2 {
			t.Fatalf("after settleDelay: assistant_view_block count=%d, want 2 (%q)", got, f.eventTypes())
		}
	})
}

func TestInlineAgyMainRecoversMissedChoices(t *testing.T) {
	dsn := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, dsn, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	f := newFakeHub().withMachine()
	const machine = "server"
	conversation := "agy-main-" + randomHex(8)
	f.settings = &Settings{Main: conversation, MainMachineID: machine, MachineID: machine, Launcher: "agy", Timezone: "Asia/Shanghai"}
	d := newFleetDevs()
	d.add(machine, "Test computer")
	s := withSettings(t, f, New(Options{Hub: f, Store: func() Store { return repositorystore.New(database) }, Logf: testLogf(t), Devices: d}))
	ft := &fleetTest{t: t, s: s, d: d, f: f}
	m, _ := s.fleetMachine(machine)
	row := proto.Session{SessionID: conversation, Cwd: m.assistantCwd(), Launcher: "agy", Status: proto.StatusDone, Rev: 1111, UpdatedAt: time.Now().UTC()}
	ref := SessionRef{MachineID: machine, SessionID: conversation}
	card := `{"fallback":"Choose","sections":[{"items":[{"type":"row","title":"One","action":{"id":"one","reply":"Trusted first choice"}},{"type":"row","title":"Two","action":{"id":"two","reply":"Trusted second choice"}}]}]}`
	text := "Options:\n```persona-view\n" + card + "\n```\n"
	it := proto.Item{ID: "1111@native#1", Seq: 1111, TS: row.UpdatedAt, Role: "assistant", Kind: proto.KindText, Text: text}
	const key = "1111@native#1/0"
	var tail atomic.Pointer[historyPage]
	readTail := func(_ string, _ json.RawMessage) (any, error) { return tail.Load(), nil }
	f.answerNode(DeviceSessionItems, readTail)
	seq := it.Seq
	oldSeq := seq - 1
	d.setSessions(machine, []proto.Session{row})
	keys := func() []string {
		result, err := database.AssistantViewBlockKeys(ctx, conversation)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, unavailable := range []historyPage{
		{Items: []proto.Item{it}, ToSeq: &seq, Partial: true},
		{Items: []proto.Item{it}, ToSeq: &oldSeq},
		{Items: []proto.Item{{ID: "1111@wrong#0", Seq: seq, Role: "user", Kind: proto.KindText, Text: text}}, ToSeq: &seq},
		{Items: []proto.Item{{ID: "1111@draft#0", Seq: seq, Role: "assistant", Kind: proto.KindText, Text: "```persona-view\n" + card}}, ToSeq: &seq},
	} {
		// An unreadable or unauthorized block is never published.
		row.UpdatedAt = row.UpdatedAt.Add(time.Second)
		d.setSessions(machine, []proto.Session{row})
		tail.Store(&unavailable)
		s.reconcileInlineMain(ctx, ref)
		if got := keys(); len(got) != 0 {
			t.Fatalf("registered an unverified card: %v", got)
		}
	}
	row.UpdatedAt = row.UpdatedAt.Add(time.Second)
	d.setSessions(machine, []proto.Session{row})
	tail.Store(&historyPage{Items: []proto.Item{it}, ToSeq: &seq})
	f.answerNode(DeviceSessionItems, func(_ string, _ json.RawMessage) (any, error) {
		newer := row
		newer.Status, newer.Rev = proto.StatusWorking, row.Rev+1
		d.setSessions(machine, []proto.Session{newer})
		return tail.Load(), nil
	})
	s.reconcileInlineMain(ctx, ref)
	if got := keys(); len(got) != 0 {
		t.Fatalf("registered the old reply after a newer turn started: %v", got)
	}
	f.answerNode(DeviceSessionItems, readTail)
	d.setSessions(machine, []proto.Session{row})

	post := func(cardKey, body, role string) *httptest.ResponseRecorder {
		path := "/api/v1/assistant/conversations/" + conversation + "/views/" + url.PathEscape(cardKey) + "/actions"
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set(RoleHeader, role)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	const input = `{"action":"two","client_msg_id":"synthetic-answer","expected_version":1}`
	if w := post(key, input, roleFull); w.Code != http.StatusNotFound {
		t.Fatalf("unregistered card press: %d %s", w.Code, w.Body.String())
	}

	feed := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/assistant/conversations/"+conversation+"/feed?paginate=1", nil)
		r.Header.Set(RoleHeader, roleFull)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := feed(); w.Code != http.StatusOK {
		t.Fatalf("main feed: %d %s", w.Code, w.Body.String())
	}
	waitFor(t, "native card registration notification", func() bool {
		return slices.Equal(keys(), []string{key}) && strings.Count(f.eventTypes(), "assistant_view_block") == 1
	})
	var snapshot struct {
		ViewBlocks []string `json:"view_blocks"`
	}
	if w := feed(); w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || !slices.Equal(snapshot.ViewBlocks, []string{key}) {
		t.Fatalf("main feed after notification: %d %s", w.Code, w.Body.String())
	}
	if w := post(key, input, roleExtension); w.Code != http.StatusForbidden {
		t.Fatalf("extension card press: %d %s", w.Code, w.Body.String())
	}
	if w := post(key, strings.Replace(input, `"expected_version":1`, `"expected_version":2`, 1), roleFull); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "view_changed") {
		t.Fatalf("stale version: %d %s", w.Code, w.Body.String())
	}
	if w := post(key, input, roleFull); w.Code != http.StatusOK {
		t.Fatalf("reconciled card press: %d %s", w.Code, w.Body.String())
	}
	waitFor(t, "trusted choice delivery", func() bool {
		a, err := database.AssistantViewAction(ctx, conversation, key)
		return err == nil && a != nil && a.Status == "sent" && strings.HasPrefix(a.Text, "Trusted second choice")
	})

	// A later load of the same native content must keep the one receipt even if the row's timestamp advances.
	row.UpdatedAt = row.UpdatedAt.Add(time.Second)
	d.setSessions(machine, []proto.Session{row})
	s.reconcileInlineMain(ctx, ref)
	if w := post(key, strings.Replace(input, `"action":"two"`, `"action":"one"`, 1), roleFull); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already_answered") {
		t.Fatalf("second choice after replay: %d %s", w.Code, w.Body.String())
	}
	if got := keys(); !slices.Equal(got, []string{key}) || strings.Count(f.eventTypes(), "assistant_view_block") != 1 {
		t.Fatalf("registration replay: keys %v, events %s", got, f.eventTypes())
	}

	// The first settled list can already contain the next final reply without an observed working-to-done edge.
	next := it
	next.ID, next.Seq, next.TS = "1112@native#1", it.Seq+1, row.UpdatedAt.Add(time.Second)
	row.Rev, row.UpdatedAt = int64(next.Seq), next.TS
	tail.Store(&historyPage{Items: []proto.Item{next}, ToSeq: &next.Seq})
	ft.sessions(machine, row)
	waitFor(t, "first settled list registration", func() bool {
		return slices.Equal(keys(), []string{key, next.ID + "/0"})
	})
}
