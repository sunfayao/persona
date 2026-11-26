package assistant

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Workspace discovery is the manager's: only the manager reaches the computers (devices.go). Its route is
// assistantwire's workspaceRoutes, relayed by the hub (503 assistant_offline while the manager is away).

// A separate store capability keeps older/test stores explicit: snapshots never fall back to memory.
type workspaceSnapshotStore interface {
	ListWorkspaceSnapshots(context.Context) ([]db.WorkspaceSnapshotRow, error)
	SaveWorkspaceSnapshot(context.Context, string, json.RawMessage) error
}

// workspaceBook serializes workspace discovery so a slow earlier request cannot overwrite a later snapshot.
type workspaceBook struct {
	discoverMu sync.Mutex
}

func (s *Service) workspaceRoutes() {
	s.handle("GET /api/v1/workspaces", s.getWorkspaces)
}

func (s *Service) snapshotStore(w http.ResponseWriter) workspaceSnapshotStore {
	st, ok := s.store().(workspaceSnapshotStore)
	if !ok || st == nil {
		apierr.WriteCode(w, 503, "not_supported", i18n.Err("hub-workspaces.err_no_store"))
		return nil
	}
	return st
}

// workspaceMachine is a computer as the board needs it: its routable id and whether its node is connected.
type workspaceMachine struct {
	ID     string
	Online bool
}

// workspaceMachines are the computers the devices carry.
func (s *Service) workspaceMachines() []workspaceMachine {
	if s.devs == nil {
		return nil
	}
	var out []workspaceMachine
	for _, d := range s.devs.List() {
		out = append(out, workspaceMachine{ID: d.ID, Online: d.Online})
	}
	return out
}

// workspaceMachineOf is the computer id names (an id another one absorbed: that one); false when there is none.
func (s *Service) workspaceMachineOf(id string) (workspaceMachine, bool) {
	if s.devs == nil || id == "" || !s.devs.Has(id) {
		return workspaceMachine{}, false
	}
	canon := s.devs.Canonical(id)
	for _, m := range s.workspaceMachines() {
		if m.ID == canon {
			return m, true
		}
	}
	return workspaceMachine{}, false
}

func (s *Service) workspaceMachineOnline(id string) bool {
	m, ok := s.workspaceMachineOf(id)
	return ok && m.Online
}

func (s *Service) effectiveMachine(id string) string {
	if m, ok := s.workspaceMachineOf(id); ok {
		return m.ID
	}
	return id
}

func workspaceDirectory(p string) string {
	if canonical, err := canonicalCwd(p); err == nil {
		return canonical
	}
	return p
}

func qualifyWorkspace(machine string, w proto.Workspace) proto.Workspace {
	w.Cwd = workspaceDirectory(w.Cwd)
	if w.Source == "" {
		w.Source = "workspace"
	}
	if w.SourceID == "" {
		w.SourceID = w.ID
	}
	if w.SourceID == "" {
		w.SourceID = w.Cwd
	}
	identity := machine + "\x00" + w.Source + "\x00" + w.SourceID
	if w.Source == "codex_app" || w.Source == proto.SourceClaudeDesktop {
		// A native desktop project can have several roots, each selectable.
		identity += "\x00" + cwdKey(w.Cwd)
	}
	sum := sha256.Sum256([]byte(identity))
	w.ID, w.MachineID = "ws_"+hex.EncodeToString(sum[:12]), machine
	return w
}

func sessionWorkspaceDirectory(s proto.Session) string {
	if s.WorkspaceCwd != "" {
		return workspaceDirectory(s.WorkspaceCwd)
	}
	return workspaceDirectory(s.Cwd)
}

// workspaceSessions are, of a machine's sessions, the first to name each directory (WorkspaceCwd, else Cwd), in list order,
// and how many its node listed. Discovery reads a directory's first session only: a later one in the same directory
// is either taken as that first one was or skipped, since sessionWorkspaceDirectory canonicalizes a path to one it leaves
// as it is. So a machine listing thousands of sessions in a few directories copies a few.
func (s *Service) workspaceSessions(machine string) ([]proto.Session, int) {
	named := map[string]bool{}
	return s.nodeSessionsWhere(machine, func(sess *proto.Session) bool {
		dir := cmp.Or(sess.WorkspaceCwd, sess.Cwd)
		if dir == "" || named[dir] {
			return false // none (sessionWorkspaceDirectory ""), or one already read
		}
		named[dir] = true
		return true
	})
}

func workspaceForSession(machine string, s proto.Session) proto.Workspace {
	title := s.SpaceName
	if title == "" {
		title = s.Cwd
	}
	cwd := sessionWorkspaceDirectory(s)
	return qualifyWorkspace(machine, proto.Workspace{Source: "session-directory", SourceID: cwd, Title: title, Cwd: cwd})
}

type workspaceDiscoveryError struct {
	MachineID string `json:"machine_id"`
	Error     string `json:"error"`
}

func (s *Service) getWorkspaces(w http.ResponseWriter, r *http.Request) {
	l := s.requestLang(r)
	store := s.snapshotStore(w)
	if store == nil {
		return
	}
	// Serialize discovery so a slow earlier request cannot overwrite a later snapshot in this hub.
	s.workspaces.discoverMu.Lock()
	defer s.workspaces.discoverMu.Unlock()
	cached, err := store.ListWorkspaceSnapshots(r.Context())
	if err != nil {
		apierr.Write(w, 503, err)
		return
	}
	// Keep the newest raw snapshot per effective machine. Decode and qualify it only when discovery needs a fallback.
	byMachine := map[string]db.WorkspaceSnapshotRow{}
	for _, row := range cached {
		machine := s.effectiveMachine(row.MachineID)
		if previous, ok := byMachine[machine]; ok {
			// Windows can stamp successive snapshots with the same clock value.
			// On a tie, the current machine's catalog supersedes an absorbed alias.
			if row.UpdatedAt.Before(previous.UpdatedAt) || (row.UpdatedAt.Equal(previous.UpdatedAt) && (previous.MachineID == machine || row.MachineID != machine)) {
				continue
			}
		}
		byMachine[machine] = row
	}
	type discovered struct {
		machine      workspaceMachine
		workspaces   []proto.Workspace
		err          error
		at           time.Time
		sourceErrors []proto.WorkspaceSourceError
	}
	machines := s.workspaceMachines()
	results := make(chan discovered, len(machines))
	for _, m := range machines {
		go func(m workspaceMachine) {
			v := discovered{machine: m, at: time.Now().UTC()}
			if !s.workspaceMachineOnline(m.ID) {
				results <- v
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			defer cancel()
			var response proto.WorkspacesResponse
			v.err = s.nodeCall(ctx, m.ID, "workspaces.list", map[string]any{}, &response)
			v.sourceErrors = response.Errors
			if v.err == nil {
				v.workspaces = make([]proto.Workspace, 0, len(response.Workspaces))
				for _, item := range response.Workspaces {
					item = qualifyWorkspace(m.ID, item)
					item.Online = true
					item.LastSeen = v.at.Format(time.RFC3339Nano)
					v.workspaces = append(v.workspaces, item)
				}
			}
			results <- v
		}(m)
	}
	out := []proto.Workspace{}
	errs := []workspaceDiscoveryError{}
	known := map[string]bool{}
	for range machines {
		result := <-results
		m := result.machine
		online := s.workspaceMachineOnline(m.ID)
		entries := result.workspaces
		if entries == nil {
			if old, ok := byMachine[m.ID]; ok {
				if err := json.Unmarshal(old.Data, &entries); err != nil {
					errs = append(errs, workspaceDiscoveryError{m.ID, i18n.T(l, "hub-workspaces.err_snapshot_read")})
				} else {
					for i := range entries {
						entries[i] = qualifyWorkspace(m.ID, entries[i])
					}
				}
			}
		}
		if len(result.sourceErrors) > 0 {
			failed := map[string]bool{}
			present := map[string]bool{}
			for _, item := range entries {
				present[item.ID] = true
			}
			for _, failure := range result.sourceErrors {
				failed[failure.Source] = true
				errs = append(errs, workspaceDiscoveryError{m.ID, failure.Source + ": " + failure.Error})
			}
			if old, ok := byMachine[m.ID]; ok {
				var previous []proto.Workspace
				if json.Unmarshal(old.Data, &previous) == nil {
					for _, item := range previous {
						item = qualifyWorkspace(m.ID, item)
						if failed[item.Source] && !present[item.ID] {
							item.Available = false
							entries = append(entries, item)
						}
					}
				}
			}
		}
		for i := range entries {
			entries[i] = qualifyWorkspace(m.ID, entries[i])
			entries[i].Online = online
			if result.err != nil || !online {
				entries[i].Available = false
			}
		}
		if result.err != nil {
			errs = append(errs, workspaceDiscoveryError{m.ID, i18n.Localize(result.err, l)})
		}
		sessions, listed := s.workspaceSessions(m.ID)
		containsDirectory := func(cwd string) bool {
			for _, item := range entries {
				if cwdKey(item.Cwd) == cwdKey(cwd) {
					return true
				}
			}
			return false
		}
		for _, session := range sessions {
			if sessionWorkspaceDirectory(session) == "" || containsDirectory(sessionWorkspaceDirectory(session)) {
				continue
			}
			item := workspaceForSession(m.ID, session)
			item.Online, item.Available = online, online
			if online {
				item.LastSeen = result.at.Format(time.RFC3339Nano)
			}
			entries = append(entries, item)
		}
		// Persist the merged directory catalog, not only spaces that still have a host terminal.
		if online && (result.err == nil || listed > 0) {
			raw, _ := json.Marshal(entries)
			if err := store.SaveWorkspaceSnapshot(r.Context(), m.ID, raw); err != nil {
				errs = append(errs, workspaceDiscoveryError{m.ID, i18n.T(l, "hub-workspaces.err_snapshot_save")})
			}
		}
		for _, item := range entries {
			if !known[item.ID] {
				known[item.ID] = true
				out = append(out, item)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MachineID != out[j].MachineID {
			return out[i].MachineID < out[j].MachineID
		}
		return out[i].ID < out[j].ID
	})
	apierr.WriteJSON(w, map[string]any{"workspaces": out, "errors": errs})
}

// requestLang is the language the request asked for (the hub's relay passes Accept-Language on), else the user's.
func (s *Service) requestLang(r *http.Request) i18n.Lang {
	if l := i18n.From(r.Context()); l != "" {
		return l
	}
	return i18n.FromRequest(r).Or(s.lang(r.Context()))
}

// maxWorkspacePathBytes is the longest directory path discovery takes in.
const maxWorkspacePathBytes = 8192

var errWorkspacePath = errors.New("invalid workspace path")

// canonicalCwd normalizes remote path syntax without consulting the hub filesystem.
// The node must resolve symlinks and verify the directory.
func canonicalCwd(cwd string) (string, error) {
	if len(cwd) > maxWorkspacePathBytes || strings.ContainsRune(cwd, 0) || !utf8.ValidString(cwd) {
		return "", errWorkspacePath
	}
	if len(cwd) >= 2 && ((cwd[0] >= 'a' && cwd[0] <= 'z') || (cwd[0] >= 'A' && cwd[0] <= 'Z')) && cwd[1] == ':' {
		if len(cwd) < 3 || (cwd[2] != '/' && cwd[2] != '\\') {
			return "", errWorkspacePath
		}
		return strings.ToLower(cwd[:2]) + path.Clean(strings.ReplaceAll(cwd[2:], "\\", "/")), nil
	}
	if strings.HasPrefix(cwd, "\\\\") || strings.HasPrefix(cwd, "//") {
		parts := strings.Split(strings.ReplaceAll(cwd[2:], "\\", "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
			return "", errWorkspacePath
		}
		root := "//" + strings.ToLower(parts[0]) + "/" + strings.ToLower(parts[1])
		rest := path.Clean("/" + strings.Join(parts[2:], "/"))
		if rest == "/" {
			return root, nil
		}
		return root + rest, nil
	}
	if !path.IsAbs(cwd) {
		return "", errWorkspacePath
	}
	return path.Clean(cwd), nil
}

// cwdKey compares remote Windows directories without changing their displayed
// spelling. POSIX paths remain case-sensitive, even when the hub runs on Windows.
func cwdKey(cwd string) string {
	canonical, err := canonicalCwd(cwd)
	if err != nil {
		return cwd
	}
	drive := len(canonical) >= 3 && canonical[0] >= 'a' && canonical[0] <= 'z' && canonical[1] == ':' && canonical[2] == '/'
	if strings.HasPrefix(canonical, "//") || drive {
		return strings.ToLower(canonical)
	}
	return canonical
}
