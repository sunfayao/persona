package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/db/dbtest"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

type testClock struct {
	wallClock
	now   time.Time
	ticks chan time.Time
}

func (c *testClock) Now() time.Time { return c.now }
func (c *testClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return c.ticks, func() {}
}

type testStore struct {
	*db.DB
	updates chan struct{}
}

func (s *testStore) A2AUpdate(ctx context.Context, row db.A2ARow, status time.Time, next *time.Time) (bool, error) {
	ok, err := s.DB.A2AUpdate(ctx, row, status, next)
	if ok {
		select {
		case s.updates <- struct{}{}:
		default:
		}
	}
	return ok, err
}

func (s *testStore) waitForUpdate(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ready() {
		select {
		case <-s.updates:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

type a2aTestHub struct {
	mu             sync.Mutex
	admit          sync.Mutex
	ids            atomic.Uint64
	cfg            assistantwire.A2AConfig
	clock          *testClock
	sessions       map[string][]proto.Session
	items          []proto.Item
	calls          []assistantwire.DeviceCall
	sends          []assistantwire.DeviceCall
	sent           chan struct{}
	wakes          chan assistantwire.DeviceCall
	sendErr        error
	sendRefusal    error
	revokedDevices map[string]bool
	starts         []map[string]any
	deleted        Ref
	executable     func() string
}

// TestMain gives the package's run a database of its own (dbtest), which a2aDatabase's copies sit next to.
func TestMain(m *testing.M) {
	os.Exit(dbtest.Main(m))
}

// a2aTemplate is the database a2aDatabase copies: migrated once per test binary.
var a2aTemplate struct {
	sync.Once
	name string
	err  error
}

// a2aDatabase is a new migrated database for one test: a copy of a2aTemplate, dropped with the run's own database.
func a2aDatabase(t *testing.T) string {
	t.Helper()
	own := os.Getenv(dbtest.Env)
	if own == "" {
		t.Skip(dbtest.Env + " not set")
	}
	a2aTemplate.Do(func() {
		ctx := context.Background()
		url, err := dbtest.Template(ctx, own)
		if err == nil {
			var d *db.DB
			if d, err = db.Open(ctx, url, db.ManagerSet); err == nil {
				d.Close()
				a2aTemplate.name = db.DatabaseName(url)
			}
		}
		a2aTemplate.err = err
	})
	if a2aTemplate.err != nil {
		t.Fatal(a2aTemplate.err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	url, err := dbtest.Clone(t.Context(), own, a2aTemplate.name, db.DatabaseName(own)+"_tpl_"+hex.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func a2aFixture(t *testing.T) (*Domain, *a2aTestHub, *testStore, assistantwire.A2AConfig, Caller, Ref) {
	t.Helper()
	database, err := db.Open(t.Context(), a2aDatabase(t), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	st := &testStore{DB: database, updates: make(chan struct{}, 1)}
	cfg := assistantwire.A2AConfig{Machines: []string{"mac", "box"}, PublicURL: "https://persona.example.com"}
	from, to := Ref{"mac", "caller-0001"}, Ref{"box", "runtime.worker-0001"}
	h := &a2aTestHub{
		cfg: cfg, sent: make(chan struct{}, 1),
		clock: &testClock{now: time.Now(), ticks: make(chan time.Time, 1)},
		sessions: map[string][]proto.Session{
			from.Machine: {{SessionID: from.Session, Status: proto.StatusWorking, Agent: "codex"}},
			to.Machine: {{SessionID: to.Session, Status: proto.StatusIdle, Agent: "pi",
				SourceKind: "runtime", Capabilities: &proto.SessionCapabilities{Send: true, Read: true}}},
		},
	}
	return h.domain(t, st), h, st, cfg, SessionCaller(from), to
}

func (h *a2aTestHub) domain(t *testing.T, st *testStore) *Domain {
	return New(Dependencies{
		Store: func() Store { return st }, Nodes: h, Machines: h, Visibility: h, Outbound: h, Launches: h, Admission: h,
		Clock: h.clock, RunContext: t.Context, NewSessionID: func() string { return fmt.Sprintf("session-%d", h.ids.Add(1)) }, NewID: func() string { return fmt.Sprintf("task-%d", h.ids.Add(1)) },
		StartTimeout: func() time.Duration { return 2 * time.Minute },
		Language:     func(context.Context) i18n.Lang { return i18n.En }, Logf: t.Logf,
	})
}

func (h *a2aTestHub) Call(_ context.Context, machine, method string, params, result any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	call := assistantwire.DeviceCall{Device: machine, Method: method, Params: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call)
	switch method {
	case "session.a2a.send":
		h.sends = append(h.sends, call)
		select {
		case h.sent <- struct{}{}:
		default:
		}
		return h.sendErr
	case "session.a2a.wake":
		h.wakes <- call
		return fakeResult(result, map[string]any{"session": proto.Session{SessionID: params.(map[string]any)["session_id"].(string)}})
	case assistantwire.DeviceSessionItems:
		return fakeResult(result, map[string]any{"items": h.items})
	case assistantwire.DeviceSessionStart:
		p := params.(map[string]any)
		h.starts = append(h.starts, p)
		return fakeResult(result, map[string]any{"session": proto.Session{SessionID: p["session_id"].(string)}})
	default:
		return nil
	}
}

func fakeResult(result, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func (h *a2aTestHub) sentCalls() []assistantwire.DeviceCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.sends)
}

func (h *a2aTestHub) waitForSend(t *testing.T, what string, count int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(h.sentCalls()) < count {
		select {
		case <-h.sent:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (h *a2aTestHub) setItems(items ...proto.Item) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = items
}

func (h *a2aTestHub) started() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.starts)
}

func setStatus(h *a2aTestHub, machine, id, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.sessions[machine] {
		if h.sessions[machine][i].SessionID == id {
			h.sessions[machine][i].Status = status
		}
	}
}

func (h *a2aTestHub) Session(_ context.Context, machine, id string) (proto.Session, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, row := range h.sessions[machine] {
		if row.SessionID == id {
			return row, true, nil
		}
	}
	return proto.Session{}, false, nil
}
func (h *a2aTestHub) Sessions(_ context.Context, machine string) []proto.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.sessions[machine])
}
func (h *a2aTestHub) Listed(Ref) (proto.Session, bool)                   { return proto.Session{}, false }
func (h *a2aTestHub) ExecutionMachine(_ context.Context, ref Ref) string { return ref.Machine }
func (h *a2aTestHub) Executable(context.Context, string) string {
	if h.executable != nil {
		return h.executable()
	}
	return ""
}
func (h *a2aTestHub) Delivered(error) string { return "" }

func (h *a2aTestHub) List(context.Context) []assistantwire.Device {
	return []assistantwire.Device{{ID: "mac", Name: "mac", Online: true}, {ID: "box", Name: "box", Online: true}}
}
func (h *a2aTestHub) Resolve(ctx context.Context, id string) (assistantwire.Device, bool) {
	for _, d := range h.List(ctx) {
		if d.ID == id {
			return d, true
		}
	}
	return assistantwire.Device{}, false
}
func (h *a2aTestHub) Lookup(ctx context.Context, id string) (assistantwire.Device, bool) {
	return h.Resolve(ctx, id)
}
func (h *a2aTestHub) Trusted(ctx context.Context, id string) bool {
	_, ok := h.Resolve(ctx, id)
	return ok
}
func (h *a2aTestHub) Offline(string) bool { return false }

func (h *a2aTestHub) Config(context.Context) assistantwire.A2AConfig { return h.cfg }
func (h *a2aTestHub) Available(_ context.Context, ref Ref) bool      { return h.deleted != ref }
func (h *a2aTestHub) Deleted(_ context.Context, ref Ref) bool        { return h.deleted == ref }
func (h *a2aTestHub) Archived(context.Context, Ref) bool             { return false }
func (h *a2aTestHub) Private(context.Context, Ref) bool              { return false }
func (h *a2aTestHub) PrivateLaunchPath(context.Context, string) bool { return false }
func (h *a2aTestHub) InTrack(s proto.Session) bool                   { return s.Space != "" }
func (h *a2aTestHub) HiddenByDirectory(string, proto.Session) bool   { return false }

func (h *a2aTestHub) AllowSend(context.Context, Ref, Ref, string) error { return h.sendRefusal }
func (h *a2aTestHub) AllowLaunch(context.Context, Ref) error            { return nil }
func (h *a2aTestHub) NeedsConfirmation(context.Context, Ref) bool       { return false }
func (h *a2aTestHub) Confirm(context.Context, Ref, string, string, json.RawMessage) (bool, error) {
	return true, nil
}
func (h *a2aTestHub) CredentialRevoked(_ context.Context, id string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.revokedDevices[id], nil
}

func (h *a2aTestHub) ResolveTier(context.Context, assistantwire.Device, string, string) (Run, error) {
	return Run{}, nil
}
func (h *a2aTestHub) DescribeTier(_ context.Context, launch *Launch) string { return launch.CWD }
func (h *a2aTestHub) Presets(context.Context, []assistantwire.Device) map[string]PresetList {
	return nil
}
func (h *a2aTestHub) ApplyModel(context.Context, map[string]any) error { return nil }
func (h *a2aTestHub) SelectHost(context.Context, assistantwire.Device, map[string]any) (bool, error) {
	return false, nil
}
func (h *a2aTestHub) UsesClaude(_ context.Context, launcher string) bool { return launcher == "claude" }
func (h *a2aTestHub) CompanionAway(error, bool) bool                     { return false }
func (h *a2aTestHub) StartUnknown(error) bool                            { return false }

func (h *a2aTestHub) Enabled(context.Context) bool { return true }
func (h *a2aTestHub) Lock() func() {
	h.admit.Lock()
	return h.admit.Unlock
}
func (h *a2aTestHub) CheckCapacity(context.Context, Ref, time.Time) error { return nil }

func resultText(res proto.ToolResult) string { return res.Content[0].Text }

func rpcHandler(s *Domain) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/a2a/{mid}/{sid}/rpc", s.ServeRPC)
	return mux
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "testdata", "prompts", name+".golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s\ngot: %q\nwant: %q", name, got, want)
	}
}
