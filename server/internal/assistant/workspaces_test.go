package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devices"
	"github.com/nwverse/persona/server/internal/proto"
)

// memoryBoardStore keeps the workspace snapshots in memory.
type memoryBoardStore struct {
	Store
	mu        sync.Mutex
	snapshots map[string]db.WorkspaceSnapshotRow
}

func newMemoryBoardStore() *memoryBoardStore {
	return &memoryBoardStore{snapshots: map[string]db.WorkspaceSnapshotRow{}}
}
func (s *memoryBoardStore) ListWorkspaceSnapshots(context.Context) ([]db.WorkspaceSnapshotRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []db.WorkspaceSnapshotRow{}
	for _, v := range s.snapshots {
		v.Data = append([]byte(nil), v.Data...)
		out = append(out, v)
	}
	return out, nil
}
func (s *memoryBoardStore) SaveWorkspaceSnapshot(_ context.Context, mid string, data json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[mid] = db.WorkspaceSnapshotRow{MachineID: mid, Data: append([]byte(nil), data...), UpdatedAt: time.Now()}
	return nil
}

// boardDevs are the computers as the manager's devices have them, for discovery: a node answering on each, whether it
// is connected, its sessions, and ids another one absorbed.
type boardDevs struct {
	Devices
	mu       sync.Mutex
	order    []string
	machines map[string]*boardDev
	absorbed map[string]string
}

type boardDev struct {
	id       string
	online   bool
	sessions []proto.Session
	fn       func(ctx context.Context, method string, raw json.RawMessage) (any, error)
}

func newBoardDevs() *boardDevs {
	return &boardDevs{machines: map[string]*boardDev{}, absorbed: map[string]string{}}
}

func (d *boardDevs) canon(id string) string {
	if to, ok := d.absorbed[id]; ok {
		return to
	}
	return id
}
func (d *boardDevs) Has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.machines[d.canon(id)] != nil
}
func (d *boardDevs) Canonical(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.machines[d.canon(id)] != nil {
		return d.canon(id)
	}
	return id
}
func (d *boardDevs) List() []devices.Summary {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []devices.Summary{}
	for _, id := range d.order {
		if m := d.machines[id]; m != nil {
			out = append(out, devices.Summary{ID: id, Name: id, Online: m.online})
		}
	}
	return out
}
func (d *boardDevs) Summary(id string) (devices.Summary, bool) {
	return summaryIn(d.List(), d.Canonical(id))
}
func (d *boardDevs) MachineResources(string) (*proto.Machine, *proto.MetricSample, *bool) {
	return nil, nil, nil
}

func (d *boardDevs) AppendSessions(dst []proto.Session, id string) ([]proto.Session, bool) {
	list, ok := d.Sessions(id)
	return append(dst, list...), ok
}
func (d *boardDevs) Sessions(id string) ([]proto.Session, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.machines[d.canon(id)]
	if m == nil {
		return nil, false
	}
	return append([]proto.Session(nil), m.sessions...), true
}

// SessionsWhere reads the sessions in place, as the devices do: a measure of discovery then counts no copy of the list.
func (d *boardDevs) SessionsWhere(id string, keep func(*proto.Session) bool) ([]proto.Session, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.machines[d.canon(id)]
	if m == nil {
		return nil, false
	}
	return keptWhere(m.sessions, keep), true
}
func (d *boardDevs) CallNode(ctx context.Context, id, method string, params json.RawMessage) (json.RawMessage, error) {
	d.mu.Lock()
	m := d.machines[d.canon(id)]
	d.mu.Unlock()
	if m == nil {
		return nil, devices.ErrNoMachine
	}
	if !m.online || m.fn == nil {
		return nil, apierr.ErrOffline
	}
	out, err := m.fn(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// add is a computer whose node answers with fn (nil: never connected).
func (d *boardDevs) add(id string, fn func(context.Context, string, json.RawMessage) (any, error)) *boardDev {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := &boardDev{id: id, online: fn != nil, fn: fn}
	d.machines[id] = m
	d.order = append(d.order, id)
	return m
}
func (d *boardDevs) setOnline(id string, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.machines[id].online = on
}
func (d *boardDevs) setSessions(id string, sessions []proto.Session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.machines[id].sessions = sessions
}

// newBoardService is a manager with the snapshot store and computers (st nil: no database).
func newBoardService(t *testing.T, st Store, d *boardDevs) (*Service, *fakeHub) {
	t.Helper()
	f := &fakeHub{}
	o := Options{Hub: f, Logf: testLogf(t)}
	if st != nil {
		o.Store = func() Store { return st }
	}
	if d != nil {
		o.Devices = d
	}
	return New(o), f
}

func boardCall(s *Service, method, path, body string, role string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set(RoleHeader, role)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func directoryNode(_ context.Context, method string, _ json.RawMessage) (any, error) {
	switch method {
	case "workspaces.list":
		return proto.WorkspacesResponse{Workspaces: []proto.Workspace{{ID: "empty", Source: "herdr", SourceID: "empty", Title: "Weather", Cwd: "/work/weather", Available: true}}}, nil
	default:
		return nil, errors.New("unexpected operation: " + method)
	}
}

// settingsBoardStore keeps the settings table in memory next to the snapshots.
type settingsBoardStore struct {
	*memoryBoardStore
	smu      sync.Mutex
	settings map[string][]byte
}

func newSettingsBoardStore() *settingsBoardStore {
	return &settingsBoardStore{memoryBoardStore: newMemoryBoardStore(), settings: map[string][]byte{}}
}
func (s *settingsBoardStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	raw, ok := s.settings[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
func (s *settingsBoardStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	s.smu.Lock()
	defer s.smu.Unlock()
	s.settings[key] = raw
	return err
}

// roleAnswer is an answer-only device (a watch): it may read, not change.
const roleAnswer = "answer"

func TestWorkspacesIncludeEmptyAndPersistOffline(t *testing.T) {
	store := newMemoryBoardStore()
	d := newBoardDevs()
	s, _ := newBoardService(t, store, d)
	d.add("laptop", directoryNode)
	d.add("server", directoryNode)
	w := boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var result struct {
		Workspaces []proto.Workspace `json:"workspaces"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Workspaces) != 2 || result.Workspaces[0].ID == result.Workspaces[1].ID {
		t.Fatalf("empty or cross-machine spaces lost: %s", w.Body)
	}
	chosen := result.Workspaces[0]
	if chosen.MachineID != "laptop" {
		t.Fatal(chosen)
	}
	d.setOnline("laptop", false)
	w = boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	for _, ws := range result.Workspaces {
		if ws.MachineID == "laptop" && (ws.Online || ws.Available || ws.LastSeen == "") {
			t.Fatalf("offline snapshot misrepresented: %+v", ws)
		}
	}
	rd := newBoardDevs()
	rd.add("laptop", nil)
	restarted, _ := newBoardService(t, store, rd)
	w = boardCall(restarted, "GET", "/api/v1/workspaces", "", roleFull)
	if !strings.Contains(w.Body.String(), chosen.ID) {
		t.Fatalf("restart lost last workspace: %s", w.Body)
	}
}

func TestWorkspacesSessionOnlySurvivesUnavailableHost(t *testing.T) {
	store := newMemoryBoardStore()
	d := newBoardDevs()
	s, _ := newBoardService(t, store, d)
	d.add("laptop", func(ctx context.Context, method string, p json.RawMessage) (any, error) {
		if method == "workspaces.list" {
			return nil, errors.New("host not running")
		}
		return directoryNode(ctx, method, p)
	})
	d.setSessions("laptop", []proto.Session{{SessionID: "external", Cwd: "/work/weather", SpaceName: "Weather"}})
	w := boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	var list proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || !list.Workspaces[0].Available {
		t.Fatalf("session-only directory lost: %s", w.Body)
	}
	// Discovery, including an external session directory, was already committed before restart.
	rd := newBoardDevs()
	rd.add("laptop", nil)
	restarted, _ := newBoardService(t, store, rd)
	w = boardCall(restarted, "GET", "/api/v1/workspaces", "", roleFull)
	var restored proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &restored)
	if len(restored.Workspaces) != 1 || restored.Workspaces[0].LastSeen == "" || restored.Workspaces[0].Online {
		t.Fatalf("restart lost session snapshot: %s", w.Body)
	}
}

func TestWorkspacesReadOnlyDevicesReadAndNoStoreIs503(t *testing.T) {
	d := newBoardDevs()
	s, _ := newBoardService(t, newMemoryBoardStore(), d)
	d.add("laptop", directoryNode)
	if w := boardCall(s, "GET", "/api/v1/workspaces", "", roleAnswer); w.Code != 200 {
		t.Fatalf("read-only GET: %d %s", w.Code, w.Body)
	}
	unsupported, _ := newBoardService(t, nil, nil)
	if w := boardCall(unsupported, "GET", "/api/v1/workspaces", "", roleFull); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

// An id another computer absorbed after its snapshot was taken is read as the computer that absorbed it.
func TestWorkspacesLateMachineAlias(t *testing.T) {
	store := newMemoryBoardStore()
	d := newBoardDevs()
	s, _ := newBoardService(t, store, d)
	weather := qualifyWorkspace("alias", proto.Workspace{Source: "herdr", SourceID: "weather", Cwd: "/work/weather", Title: "Weather"})
	extra := qualifyWorkspace("alias", proto.Workspace{Source: "herdr", SourceID: "empty", Cwd: "/work/empty", Title: "Empty"})
	raw, _ := json.Marshal([]proto.Workspace{weather, extra})
	_ = store.SaveWorkspaceSnapshot(context.Background(), "alias", raw)
	d.add("canonical", directoryNode)
	d.absorbed["alias"] = "canonical"
	d.setOnline("canonical", false)
	var list proto.WorkspacesResponse
	w := boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 2 {
		t.Fatalf("absorbed alias snapshot lost: %s", w.Body)
	}
	for _, item := range list.Workspaces {
		if item.MachineID != "canonical" || item.Online || item.Available {
			t.Fatalf("cached alias routing incorrect: %+v", item)
		}
	}
	// A newer complete catalog wins, rather than reviving deleted alias spaces.
	_ = store.SaveWorkspaceSnapshot(context.Background(), "canonical", json.RawMessage(`[]`))
	w = boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 0 {
		t.Fatalf("stale alias catalog revived: %s", w.Body)
	}
}

func TestCanonicalCwd(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{`C:\repo\one\..\two\`, `c:/repo/two`}, {`c:/repo/two`, `c:/repo/two`}, {`\\Server\Share\repo\..\two`, `//server/share/two`}, {`/unix/back\slash`, `/unix/back\slash`}, {`C:\`, `c:/`}} {
		got, err := canonicalCwd(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%q = %q, %v want %q", tc.in, got, err, tc.want)
		}
	}
	for _, p := range []string{`C:repo`, `C:`, `\repo`, `\\server`, `relative`} {
		if _, err := canonicalCwd(p); !errors.Is(err, errWorkspacePath) {
			t.Fatalf("accepted relative %q: %v", p, err)
		}
	}
	if cwdKey(`C:\Repo`) != cwdKey(`c:/repo`) || cwdKey("/Repo") == cwdKey("/repo") {
		t.Fatal("Windows paths compare without case, POSIX paths with it")
	}
}

func TestWorkspacesFailedSourcesKeepQualifiedAliasSnapshot(t *testing.T) {
	store := newMemoryBoardStore()
	main, unread := true, false
	cached := []proto.Workspace{
		{ID: "retained", Source: "orca", SourceID: "retained", Title: "Retained", Cwd: "/work/retained/.",
			ParentSourceID: "root", ProjectID: "project", ProjectName: "Project", ProjectPath: "/work/project",
			IsMain: &main, Unread: &unread, SidebarStatus: "working", WorkspaceKind: "worktree", Head: "example", Index: 3,
			Branch: "topic", Online: true, Available: true, LastSeen: "2026-01-01T00:00:00Z"},
		{ID: "current", Source: "orca", SourceID: "current", Title: "Old title", Cwd: "/work/current"},
		{ID: "legacy", MachineID: "alias", Title: "Legacy", Cwd: "/work/legacy/.", LastSeen: "2026-01-01T00:00:00Z"},
		{ID: "removed", Source: "herdr", SourceID: "removed", Title: "Removed", Cwd: "/work/removed"},
	}
	cached[0] = qualifyWorkspace("alias", cached[0])
	cached[1] = qualifyWorkspace("alias", cached[1])
	raw, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	// The latest alias catalog wins even when an older canonical snapshot was listed first.
	store.snapshots["canonical"] = db.WorkspaceSnapshotRow{MachineID: "canonical", Data: json.RawMessage(`[]`), UpdatedAt: time.Unix(1, 0)}
	store.snapshots["alias"] = db.WorkspaceSnapshotRow{MachineID: "alias", Data: raw, UpdatedAt: time.Unix(2, 0)}
	d := newBoardDevs()
	d.absorbed["alias"] = "canonical"
	current := proto.Workspace{ID: "current", Source: "orca", SourceID: "current", Title: "Current", Cwd: "/work/current", Available: true}
	d.add("canonical", func(context.Context, string, json.RawMessage) (any, error) {
		return proto.WorkspacesResponse{Workspaces: []proto.Workspace{current}, Errors: []proto.WorkspaceSourceError{
			{Source: "orca", Error: "catalog unavailable"}, {Source: "workspace", Error: "catalog unavailable"},
		}}, nil
	})
	s, _ := newBoardService(t, store, d)
	read := func(t *testing.T, service *Service, expectedErrors int) map[string]proto.Workspace {
		t.Helper()
		w := boardCall(service, "GET", "/api/v1/workspaces", "", roleFull)
		var response struct {
			Workspaces []proto.Workspace         `json:"workspaces"`
			Errors     []workspaceDiscoveryError `json:"errors"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
			t.Fatalf("workspace catalog: HTTP %d, error %v, body %s", w.Code, err, w.Body)
		}
		if len(response.Workspaces) != 3 || len(response.Errors) != expectedErrors {
			t.Fatalf("workspace catalog lost cached rows or errors: %s", w.Body)
		}
		for _, failure := range response.Errors {
			if failure.MachineID != "canonical" {
				t.Fatalf("failure retained an alias: %+v", failure)
			}
		}
		out := map[string]proto.Workspace{}
		for _, item := range response.Workspaces {
			out[item.ID] = item
		}
		return out
	}
	got := read(t, s, 2)
	want := map[string]proto.Workspace{}
	for _, item := range []proto.Workspace{current, cached[0], cached[2]} {
		item = qualifyWorkspace("canonical", item)
		item.Online = true
		if item.SourceID == "current" {
			item.LastSeen = got[item.ID].LastSeen
			if _, err := time.Parse(time.RFC3339Nano, item.LastSeen); err != nil {
				t.Fatal("current workspace lost its discovery time")
			}
		} else {
			item.Available = false
		}
		want[item.ID] = item
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qualified failed-source catalog: got %+v, want %+v", got, want)
	}
	// Qualification must precede duplicate checks, not just final response encoding: the stored catalog has no
	// second copy of the current row, preserves Orca metadata and drops the successful source's deleted space.
	var persisted []proto.Workspace
	if err := json.Unmarshal(store.snapshots["canonical"].Data, &persisted); err != nil || len(persisted) != len(want) {
		t.Fatalf("persisted catalog has duplicate or missing rows: %+v, error %v", persisted, err)
	}
	for _, item := range persisted {
		if !reflect.DeepEqual(item, want[item.ID]) {
			t.Fatalf("persisted workspace differs: %+v", item)
		}
	}
	if string(store.snapshots["alias"].Data) != string(raw) {
		t.Fatal("projecting an alias changed its durable snapshot")
	}
	saved := string(store.snapshots["canonical"].Data)
	for _, c := range []struct {
		name   string
		online bool
		errors int
	}{{"offline restart", false, 0}, {"failed catalog restart", true, 1}} {
		t.Run(c.name, func(t *testing.T) {
			rd := newBoardDevs()
			rd.absorbed["alias"] = "canonical"
			if c.online {
				rd.add("canonical", func(context.Context, string, json.RawMessage) (any, error) {
					return nil, errors.New("catalog unavailable")
				})
			} else {
				rd.add("canonical", nil)
			}
			restarted, _ := newBoardService(t, store, rd)
			got := read(t, restarted, c.errors)
			for id, item := range want {
				item.Online, item.Available = c.online, false
				if !reflect.DeepEqual(got[id], item) {
					t.Fatalf("restart changed cached metadata or identity: got %+v, want %+v", got[id], item)
				}
			}
			if string(store.snapshots["canonical"].Data) != saved {
				t.Fatal("an unavailable node rewrote its last successful snapshot")
			}
		})
	}
}

func TestWorkspacesUseVerifiedSessionDirectory(t *testing.T) {
	d := newBoardDevs()
	s, _ := newBoardService(t, newMemoryBoardStore(), d)
	d.add("laptop", directoryNode)
	sessions := []proto.Session{{SessionID: "external", Cwd: "/work/alias", WorkspaceCwd: "/work/weather"}}
	d.setSessions("laptop", sessions)
	w := boardCall(s, "GET", "/api/v1/workspaces", "", roleFull)
	var list proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || list.Workspaces[0].Cwd != "/work/weather" {
		t.Fatalf("symlink produced duplicate directories: %s", w.Body)
	}
	candidate := workspaceForSession("laptop", sessions[0])
	if candidate.Cwd != "/work/weather" || candidate.SourceID != "/work/weather" {
		t.Fatalf("session fallback ignored verified path: %+v", candidate)
	}
}

func TestPaseoWorkspacePinsSurviveSnapshotsAndClearOnRefresh(t *testing.T) {
	store := newMemoryBoardStore()
	d := newBoardDevs()
	pin := "2026-11-16T13:00:00Z"
	d.add("laptop", func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "workspaces.list" {
			return nil, errors.New("unexpected workspace mutation")
		}
		return proto.WorkspacesResponse{Workspaces: []proto.Workspace{
			{ID: "pinned", Source: "paseo", SourceID: "pinned", ProjectID: "project", Cwd: "/w/app", PinnedAt: pin, Available: true},
			{ID: "sibling", Source: "paseo", SourceID: "sibling", ProjectID: "project", Cwd: "/w/app", Available: true},
		}}, nil
	})
	s, _ := newBoardService(t, store, d)
	read := func(service *Service) []proto.Workspace {
		t.Helper()
		w := boardCall(service, "GET", "/api/v1/workspaces", "", roleFull)
		var result proto.WorkspacesResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Workspaces) != 2 {
			t.Fatalf("workspace catalog: %d %s", w.Code, w.Body)
		}
		return result.Workspaces
	}
	assertPin := func(rows []proto.Workspace, expected string) {
		t.Helper()
		for _, w := range rows {
			want := ""
			if w.SourceID == "pinned" {
				want = expected
			}
			if w.PinnedAt != want || w.MachineID != "laptop" || w.Source != "paseo" {
				t.Fatalf("native pin lost or assigned to its sibling: %+v", w)
			}
		}
	}
	assertPin(read(s), pin)
	d.setOnline("laptop", false)
	restarted, _ := newBoardService(t, store, d)
	assertPin(read(restarted), pin)
	pin = ""
	d.setOnline("laptop", true)
	assertPin(read(restarted), "")
	d.setOnline("laptop", false)
	assertPin(read(restarted), "")
}
