package hub

// What one manager may not do to another's (#5556): claim its machine or its sessions, lose its moves, keep moving
// while being removed. Each test has two managers on a Postgres bus hub (newBusHub).

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/bus"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// forgetTodoOwner drops a to-do's cached owner (the hub itself keeps them; the test checks the cache it reads).
func (h *Hub) forgetTodoOwner(id string) {
	if id == "" {
		return
	}
	h.todoOwnersMu.Lock()
	delete(h.todoOwners, id)
	h.todoOwnersMu.Unlock()
}

// guardManager is a manager on the bus that answers the hub's calls with answer and keeps a log of them; onEvent
// (f.mu), when set, is told the hub's events (heard).
type guardManager struct {
	id      string
	token   string // its bus token
	peer    *rpc.Peer
	mu      sync.Mutex
	got     []string
	onEvent func(method string, params json.RawMessage)
}

func connectGuardManager(t *testing.T, h *Hub, url, id string, answer func(method string, params json.RawMessage) (any, error)) *guardManager {
	t.Helper()
	return connectGuardManagerWith(t, url, id, busToken(t, h, id, id), answer)
}

// connectGuardManagerWith is connectGuardManager with a bus token the manager has (connecting again with it).
func connectGuardManagerWith(t *testing.T, url, id, token string, answer func(method string, params json.RawMessage) (any, error)) *guardManager {
	t.Helper()
	f := &guardManager{id: id, token: token}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ws, err := bus.Dial(ctx, url, f.token, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	conn := bus.NewConn(ctx, ws)
	f.peer = rpc.New("g", conn)
	f.peer.OnEvent = func(method string, params json.RawMessage) { // set before Serve; the hook is f.onEvent
		f.mu.Lock()
		fn := f.onEvent
		f.mu.Unlock()
		if fn != nil {
			fn(method, params)
		}
	}
	f.peer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		f.mu.Lock()
		f.got = append(f.got, method+" "+string(params))
		f.mu.Unlock()
		return answer(method, params)
	}
	go func() { _ = f.peer.Serve(ctx, conn) }()
	cctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cc()
	if err := f.peer.Call(cctx, busHello, map[string]any{"id": id}, nil); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *guardManager) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.got, "\n")
}

func (f *guardManager) call(t *testing.T, method string, params, out any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return f.peer.Call(ctx, method, params, out)
}

func (f *guardManager) report(t *testing.T, states ...devicewire.State) {
	t.Helper()
	for i := range states {
		states[i].Machine.Online, states[i].Connected, states[i].ConnectedAt = true, true, time.Now().UTC()
	}
	if err := f.peer.Notify(devicewire.EventState, devicewire.States{Machines: states}); err != nil {
		t.Fatal(err)
	}
}

func answerWho(id string) func(string, json.RawMessage) (any, error) {
	return func(string, json.RawMessage) (any, error) { return map[string]any{"who": id}, nil }
}

func guardPost(t *testing.T, h *Hub, srv *httptest.Server, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// nodeAnswers answers devices.call as a node would for session.start: a resume of a running session answers that
// session, an async start the id it was given.
func nodeAnswers(method string, params json.RawMessage) (any, error) {
	if method != devicewire.MethodCall {
		return map[string]any{}, nil
	}
	var c devicewire.NodeCall
	_ = json.Unmarshal(params, &c)
	if c.Method == assistantwire.DeviceSessionStart {
		var p struct {
			Resume string `json:"resume_session_id"`
			ID     string `json:"session_id"`
		}
		_ = json.Unmarshal(c.Params, &p)
		return map[string]any{"session": map[string]any{"session_id": cmp.Or(p.Resume, p.ID, "fresh-1")}}, nil
	}
	return map[string]any{"who": "a", "node": c.Method}, nil
}

// With one manager nothing changes: a snapshot view naming no machine (an old row) counts for no manager, so a request
// about it goes where it always did (the default manager, the only one), and the session lists are stamped with it.
func TestAViewWithoutAMachineWithOneManager(t *testing.T) {
	h, srv, _ := newBusHub(t)
	a := connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, nodeAnswers)
	a.report(t, devicewire.State{Machine: proto.Machine{ID: "ma", Name: "ma"}})
	eventually(t, 5*time.Second, "ma", func() bool { return h.localMachine("ma") != nil })
	var res assistantwire.SnapshotResult
	if err := a.call(t, assistantwire.MethodSnapshot, assistantwire.Snapshot{Epoch: "a", Seq: 1,
		Sessions: []assistantwire.SessionView{{SessionID: "old", Title: "An old one"}}}, &res); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/assistant/conversations/old", nil)
	r.SetPathValue("id", "old")
	if id, err := h.byConversation(r); err != nil || id != assistantwire.AssistantID || h.conversationOwner("old") != "" {
		t.Fatalf("old row: routed to %q (%v), counted for %q", id, err, h.conversationOwner("old"))
	}
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "ma", Sessions: []proto.Session{{MachineID: "ma", SessionID: "old", Title: "An old one"}}})
	if l := h.decorated(h.localMachine("ma")); len(l) != 1 || l[0].Manager != assistantwire.AssistantID || l[0].Title != "An old one" {
		t.Fatalf("listed: %+v", l)
	}
}

// Moves of different machines at the same time all stay: in memory and in the database.
func TestConcurrentMovesAllStay(t *testing.T) {
	h, srv, store := newBusHub(t)
	var ms []MachineConfig
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8"} {
		ms = append(ms, MachineConfig{ID: id, Name: id, Kind: "local"})
	}
	h.cfg.Machines = ms
	connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, answerWho("a"))
	connectGuardManager(t, h, srv.URL, "m-0b0b0b", answerWho("b"))
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		for _, m := range ms {
			wg.Add(1)
			go func(mid string) {
				defer wg.Done()
				if _, err := h.moveMachine(context.Background(), mid, "m-0b0b0b"); err != nil {
					t.Error(err)
				}
			}(m.ID)
		}
		wg.Wait()
		var kept map[string]string
		_, _ = store.Setting(context.Background(), machineOwnersKey, &kept)
		for _, m := range ms {
			if h.entryOwner(m.ID) != "m-0b0b0b" || kept[m.ID] != "m-0b0b0b" {
				t.Fatalf("round %d: %s in memory %s, kept %q", round, m.ID, h.entryOwner(m.ID), kept[m.ID])
			}
		}
		for _, m := range ms {
			if _, err := h.moveMachine(context.Background(), m.ID, devicesOwner); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// failingOwners is the database, but reading who carries which machine fails while fail is set.
type failingOwners struct {
	*db.DB
	fail bool
}

func (f *failingOwners) Setting(ctx context.Context, key string, out any) (bool, error) {
	if f.fail && key == machineOwnersKey {
		return false, errors.New("database away")
	}
	return f.DB.Setting(ctx, key, out)
}

// While the hub could not read who carries which machine, no move: it would overwrite what is kept. Once it can,
// the move adds to it; an owner the roster no longer has, and a machine hub.json no longer has, are dropped.
func TestNoMoveUntilTheOwnersAreRead(t *testing.T) {
	h, srv, store := newBusHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "m1", Name: "m1", Kind: "local"}, {ID: "m2", Name: "m2", Kind: "local"}}
	connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, answerWho("a"))
	connectGuardManager(t, h, srv.URL, "m-0b0b0b", answerWho("b"))
	if err := store.PutSetting(context.Background(), machineOwnersKey, map[string]string{"m2": "m-0b0b0b", "gone": "m-0b0b0b", "m1": "m-ffffff"}); err != nil {
		t.Fatal(err)
	}
	f := &failingOwners{DB: store, fail: true}
	h.bus.idents = f
	if _, err := h.moveMachine(context.Background(), "m1", "m-0b0b0b"); !errors.Is(err, errOwnersUnknown) {
		t.Fatalf("move while unread: %v", err)
	}
	if code, out := guardPost(t, h, srv, "/api/v1/managers/m-0b0b0b/machines", `{"machine_id":"m1"}`); code != http.StatusServiceUnavailable || out["code"] != "owners_unknown" {
		t.Fatalf("route: %d %v", code, out)
	}
	f.fail = false
	if _, err := h.moveMachine(context.Background(), "m1", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	var kept map[string]string
	_, _ = store.Setting(context.Background(), machineOwnersKey, &kept)
	if len(kept) != 2 || kept["m1"] != "m-0b0b0b" || kept["m2"] != "m-0b0b0b" {
		t.Fatalf("kept %v", kept)
	}
}

// Removing a manager waits for a move to it, and a move after finds it gone; its machines go back to the carrier, its
// cards go.
func TestRemovingAManagerAgainstItsMoves(t *testing.T) {
	h, srv, _ := newBusHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "m1", Name: "m1", Kind: "local"}}
	connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, answerWho("a"))
	b := connectGuardManager(t, h, srv.URL, "m-0b0b0b", answerWho("b"))
	if _, err := h.moveMachine(context.Background(), "m1", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	if err := b.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: proto.Interaction{
		ID: "ix-b", MachineID: "m1", SessionID: "s", Kind: "question", CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "its card", func() bool { return h.interactionOwner("ix-b") == "m-0b0b0b" })
	// A removal holds the moves back; the move waiting finds it gone.
	h.moving.managers.Lock()
	moved := make(chan error, 1)
	go func() { _, err := h.moveMachine(context.Background(), "m1", "m-0b0b0b"); moved <- err }()
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-moved:
		t.Fatalf("a move went on during a removal: %v", err)
	default:
	}
	h.moving.managers.Unlock()
	if err := <-moved; err != nil {
		t.Fatal(err) // still there: a no-op
	}
	if code, _ := ownershipGet(t, h, srv, http.MethodDelete, "/api/v1/managers/m-0b0b0b", nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if h.entryOwner("m1") != assistantwire.AssistantID || h.interactionOwner("ix-b") != "" {
		t.Fatalf("after: m1 %s, card %q", h.entryOwner("m1"), h.interactionOwner("ix-b"))
	}
	if _, err := h.moveMachine(context.Background(), "m1", "m-0b0b0b"); !errors.Is(err, errUnknownManager) {
		t.Fatalf("move to a removed manager: %v", err)
	}
}

// A card of one manager's is not replaced by another reporting the same id.
func TestACardIsNotReplacedByAnotherManager(t *testing.T) {
	h, srv, _ := newBusHub(t)
	a := connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, answerWho("a"))
	b := connectGuardManager(t, h, srv.URL, "m-0b0b0b", answerWho("b"))
	ix := proto.Interaction{ID: "ix", MachineID: "ma", SessionID: "s", Kind: "question", CreatedAt: time.Now(), Cwd: "/a"}
	if err := a.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "a's card", func() bool { return h.interactionOwner("ix") == assistantwire.AssistantID })
	ix.Cwd = "/b"
	_ = b.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix})
	_ = b.peer.Notify(assistantwire.EventFleetInteractions, assistantwire.FleetInteractions{Interactions: []proto.Interaction{ix}})
	time.Sleep(200 * time.Millisecond)
	if got, _ := h.pendingInteraction("ix"); got.Manager != assistantwire.AssistantID || got.Cwd != "/a" {
		t.Fatalf("replaced: %+v", got)
	}
	guardPost(t, h, srv, "/api/v1/interactions/ix/resolve", `{"allow":true}`)
	if !strings.Contains(a.log(), "/api/v1/interactions/ix/resolve") || strings.Contains(b.log(), "/api/v1/interactions/ix/resolve") {
		t.Fatalf("resolve went to the wrong manager\nA:\n%s\nB:\n%s", a.log(), b.log())
	}
}

// Two managers serving the same API prefix: the request's named manager's, else the default's.
func TestAPrefixServedByTwoManagers(t *testing.T) {
	h := newTestHub(t)
	h.keepExtReport(assistantwire.AssistantID, &extwire.Report{APIs: []extwire.APIPrefix{{Prefix: "lark"}}})
	h.keepExtReport("m-0b0b0b", &extwire.Report{APIs: []extwire.APIPrefix{{Prefix: "lark"}, {Prefix: "only-b"}}})
	for _, tc := range []struct{ named, def, want string }{
		{"", assistantwire.AssistantID, assistantwire.AssistantID},
		{"m-0b0b0b", assistantwire.AssistantID, "m-0b0b0b"},
		{"", "m-0b0b0b", "m-0b0b0b"},
	} {
		if got := h.extPrefixOwner("lark", tc.named, tc.def); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
	if got := h.extPrefixOwner("only-b", "", assistantwire.AssistantID); got != "m-0b0b0b" {
		t.Fatalf("only-b: %s", got)
	}
}

// device.call goes only to a machine of the caller's own: on another manager's, nothing (another manager's session,
// files, panes, memory, a session of its own, the session list) reaches the node; the calls about every machine list
// only the caller's machines. The machine's own manager, the assistant here, may do anything there, and nothing else:
// it is a manager like the others.
func TestDeviceCallOnlyOnOwnMachines(t *testing.T) {
	h, srv, _ := newBusHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "ma", Name: "ma", Kind: "local"}, {ID: "mb", Name: "mb", Kind: "local"}}
	a := connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, nodeAnswers)
	b := connectGuardManager(t, h, srv.URL, "m-0b0b0b", nodeAnswers)
	if _, err := h.moveMachine(context.Background(), "mb", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	a.report(t, devicewire.State{Machine: proto.Machine{ID: "ma", Name: "ma"}})
	b.report(t, devicewire.State{Machine: proto.Machine{ID: "mb", Name: "mb"}})
	eventually(t, 5*time.Second, "ma, mb", func() bool { return h.localMachine("ma") != nil && h.localMachine("mb") != nil })
	nodeSaw := func(f *guardManager, method string) bool { return strings.Contains(f.log(), `"method":"`+method+`"`) }
	for _, m := range []string{"session.send", "session.interrupt", "session.close", assistantwire.DeviceSessionItems, assistantwire.DeviceSessionStart,
		assistantwire.DeviceAssistantSend, "fs.read", "panes.list", assistantwire.DeviceAssistantMemory, assistantwire.DeviceSessionsList} {
		err := b.call(t, assistantwire.MethodDevice, assistantwire.DeviceCall{Device: "ma", Method: m,
			Params: json.RawMessage(`{"session_id":"a-agent","text":"x","path":"/x"}`)}, nil)
		if rpcCode(err) != devicewire.CodeNoDevice || nodeSaw(a, m) {
			t.Fatalf("%s on a's machine: %v (node saw it: %v)", m, err, nodeSaw(a, m))
		}
		// And the other way round: the assistant has no say on b's machine either.
		if err := a.call(t, assistantwire.MethodDevice, assistantwire.DeviceCall{Device: "mb", Method: m,
			Params: json.RawMessage(`{"session_id":"b-agent"}`)}, nil); rpcCode(err) != devicewire.CodeNoDevice || nodeSaw(b, m) {
			t.Fatalf("%s on b's machine by the assistant: %v", m, err)
		}
	}
	// On its own machine: anything, a resume and the machine's files included.
	for _, c := range []struct {
		f                   *guardManager
		dev, method, params string
	}{
		{b, "mb", "session.send", `{"session_id":"b-agent","text":"go on"}`},
		{b, "mb", assistantwire.DeviceSessionStart, `{"resume_session_id":"b-agent","cwd":"/"}`},
		{a, "ma", assistantwire.DeviceSessionStart, `{"resume_session_id":"a-agent","cwd":"/"}`},
		{a, "ma", "fs.read", `{"path":"/x"}`},
	} {
		if err := c.f.call(t, assistantwire.MethodDevice, assistantwire.DeviceCall{Device: c.dev, Method: c.method,
			Params: json.RawMessage(c.params)}, nil); err != nil || !nodeSaw(c.f, c.method) {
			t.Fatalf("%s on %s, its own machine: %v", c.method, c.dev, err)
		}
	}
	// The list of every machine is its own.
	var list struct {
		Devices []struct{ ID string } `json:"devices"`
		Others  []string              `json:"others"`
	}
	if err := b.call(t, assistantwire.MethodDevice, assistantwire.DeviceCall{Device: assistantwire.DeviceAll, Method: assistantwire.DeviceDevicesList}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Devices) != 1 || list.Devices[0].ID != "mb" || !slices.Contains(list.Others, "ma") {
		t.Fatalf("its devices: %+v", list)
	}
}

// rosterUnsure is the database, but looking up manager unsure fails.
type rosterUnsure struct {
	*db.DB
	unsure string
}

func (r *rosterUnsure) BusIdentityByID(ctx context.Context, id string) (*db.BusIdentity, error) {
	if id == r.unsure {
		return nil, errors.New("database away")
	}
	return r.DB.BusIdentityByID(ctx, id)
}

// Reading who carries which machine drops an owner only when the roster says it is gone, not when it cannot tell.
func TestOwnersKeptWhenTheRosterCannotTell(t *testing.T) {
	h, _, store := newBusHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "m1", Name: "m1", Kind: "local"}, {ID: "m2", Name: "m2", Kind: "local"}}
	if err := store.PutSetting(context.Background(), machineOwnersKey, map[string]string{"m1": "m-0e0e0e", "m2": "m-0f0f0f"}); err != nil {
		t.Fatal(err)
	}
	h.bus.idents = &rosterUnsure{DB: store, unsure: "m-0e0e0e"}
	if err := h.loadMachineOwners(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.entryOwner("m1") != "m-0e0e0e" || h.entryOwner("m2") != devicesOwner {
		t.Fatalf("m1 %s (lookup failed: kept), m2 %s (not in the roster: dropped)", h.entryOwner("m1"), h.entryOwner("m2"))
	}
}

// Asking a manager whether it has a source: an answer that names no source is no answer.
func TestASourceAnswerNamingNoSourceIsNoAnswer(t *testing.T) {
	h, srv, _ := newBusHub(t)
	connectGuardManager(t, h, srv.URL, "m-0b0b0b", func(method string, _ json.RawMessage) (any, error) {
		return map[string]any{"kind": "mail", "data": map[string]any{}}, nil
	})
	has, known := h.sourceKnownAt(context.Background(), "m-0b0b0b", "mail:z")
	if has || known {
		t.Fatalf("has %v, known %v", has, known)
	}
}

// A source's move lock is forgotten once no move of it runs or waits.
func TestASourceLockIsForgotten(t *testing.T) {
	var mv moves
	unlock := mv.lockSource("mail:x")
	waited := make(chan struct{})
	go func() {
		u := mv.lockSource("mail:x")
		close(waited)
		u()
	}()
	time.Sleep(50 * time.Millisecond)
	unlock()
	<-waited
	time.Sleep(20 * time.Millisecond)
	mv.mu.Lock()
	defer mv.mu.Unlock()
	if len(mv.sources) != 0 {
		t.Fatalf("kept: %v", mv.sources)
	}
}

func TestByTodoRoutesToItsManager(t *testing.T) {
	h := &Hub{}
	h.noteTodoOwner("t1", "m-other")
	h.noteTodoOwner("t2", "assistant")

	// When unnamed, routes to the manager that owns it
	req1 := httptest.NewRequest(http.MethodPatch, "/api/v1/todos/t1", nil)
	req1.SetPathValue("id", "t1")
	id, err := h.byTodo(req1)
	if err != nil || id != "m-other" {
		t.Fatalf("unnamed manager: got %s, %v, want m-other", id, err)
	}

	// Unnamed todo owned by assistant
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/todos/t2", nil)
	req2.SetPathValue("id", "t2")
	id, err = h.byTodo(req2)
	if err != nil || id != "assistant" {
		t.Fatalf("unnamed manager: got %s, %v, want assistant", id, err)
	}

	// Sub-paths like /retry or /decisions also use {id}
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/todos/t1/retry", nil)
	req3.SetPathValue("id", "t1")
	id, err = h.byTodo(req3)
	if err != nil || id != "m-other" {
		t.Fatalf("retry sub-path: got %s, %v, want m-other", id, err)
	}

	// Forget todo owner clears the cached routing
	h.forgetTodoOwner("t1")
	if h.todoOwner("t1") != "" {
		t.Fatal("todo t1 owner should be forgotten")
	}
}
