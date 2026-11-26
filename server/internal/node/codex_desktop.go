package node

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
	"github.com/sunfayao/persona/server/internal/pathutil"
	"github.com/sunfayao/persona/server/internal/proto"
)

const codexDesktopTimeout = 60 * time.Second

const codexDesktopUnavailableKey = "codex-desktop.control_unavailable"

// This refusal is safe to retry: no turn or settings request reached the App. Keep transport diagnostics out of
// the user-facing message, including the stale socket's path after an App update removes the bus.
func codexDesktopUnavailable(ctx context.Context) error {
	return refusal("codex_read_only", i18n.T(i18n.From(ctx), codexDesktopUnavailableKey), nil)
}

// codexDesktopLoadWait: how long a thread the App has not loaded (one a thread just created, one not opened since the
// App started) is given to load after the App is asked to open it.
var codexDesktopLoadWait = 30 * time.Second

func dialCodexDesktop(ctx context.Context, home string) (*appserver.Desktop, error) {
	return appserver.New(appserver.Config{}).DialDesktop(ctx, func(ctx context.Context) (net.Conn, error) {
		return dialCodexDesktopBus(ctx, home)
	}, newUUID)
}

// sendCodexDesktop runs a turn in a thread the Codex App has loaded.
func (n *Node) sendCodexDesktop(ctx context.Context, src codexSource, text string, images []proto.Image) error {
	// Refuse before saving attachments or holding a message behind a running turn on an unavailable App.
	if !codexDesktopAvailable(src.home) {
		return codexDesktopUnavailable(ctx)
	}
	paths, err := n.saveImages(images)
	if err != nil {
		return refusal("bad_image", err.Error(), nil)
	}
	// While the App runs a turn in the thread the message waits here and goes in once, as a turn of its own, when the
	// turn ends (codex_desktop_queue.go): a start-turn into a running turn is steered into it and then run again.
	return n.sendOrHoldCodexDesktop(ctx, src, codexDesktopMessage{text: text, paths: paths})
}

// startCodexDesktopTurn asks the window owning src's thread to run a turn with input, as the App's own composer does.
func (n *Node) startCodexDesktopTurn(ctx context.Context, src codexSource, input []map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, codexDesktopTimeout)
	defer cancel()
	c, err := dialCodexDesktop(ctx, src.home)
	if err != nil {
		return codexDesktopUnavailable(ctx)
	}
	defer c.Close()
	owner, err := loadedCodexDesktopOwner(ctx, c, src.thread)
	if err != nil {
		return err
	}
	turn := map[string]any{
		"conversationId": src.thread,
		"turnStart": map[string]any{
			"request": map[string]any{"threadId": src.thread, "clientUserMessageId": newUUID(), "input": input},
			"context": map[string]any{},
		},
	}
	if _, err := c.Request("thread-follower-start-turn", turn, owner.HandledByClient); err != nil {
		// The owner took the request: whether the turn started is not known.
		return uncertain(err)
	}
	n.notifyCodex()
	return nil
}

// interruptCodexDesktop stops the turn running in a thread the Codex App owns, as the App's own stop button does
// (thread-follower-interrupt-turn, mode "user-stop": the turn and the subagents it started). A thread no App window has
// loaded runs no turn there, and it is not opened to be stopped.
func (n *Node) interruptCodexDesktop(ctx context.Context, src codexSource) error {
	ctx, cancel := context.WithTimeout(ctx, codexDesktopTimeout)
	defer cancel()
	c, err := dialCodexDesktop(ctx, src.home)
	if err != nil {
		return codexDesktopUnavailable(ctx)
	}
	defer c.Close()
	owner, err := c.Owner(src.thread)
	if err != nil {
		return refusal("codex_read_only", "没有可停止的已观察轮次", nil)
	}
	stop := map[string]any{"conversationId": src.thread, "mode": "user-stop"}
	if _, err := c.Request("thread-follower-interrupt-turn", stop, owner.HandledByClient); err != nil {
		return err
	}
	n.notifyCodex()
	return nil
}

// The Codex App's threads are listed like its sidebar: grouped by its projects, newest first, and those of the last
// few days with a state like herdr's — working while a turn runs; after it, done ("Unread") while the App's sidebar
// still marks the thread unread, idle once it was looked at in the App (or, as for any agent, in Persona).
// A process holding the rollout open only ever said "unknown", which the phone folds away with the quiet ones.
const codexDesktopRecent = 72 * time.Hour

// codexAppState joins the App's legacy sidebar state with its native project catalog and thread assignments.
// Projects and memberships migrate independently, so both sources remain readable until that migration finishes.
type codexAppState struct {
	unread      map[string]bool
	projectless map[string]bool
	assigned    map[string]string // thread → project id
	projects    map[string]codexAppProject
	sidebar     []string // the threads of both lists that are valid ids, sorted: what codexAppThreads looks up

	order []string // the App's projects in its sidebar's order (project-order)

	namesOnce sync.Once
	names     map[string]string // a project's folder (real path) → the App's name for it; see folderName
	places    map[string]int    // a project's folder (real path) → its place in order, from 1; see folderPlace
}

type codexAppProject struct {
	Name      string   `json:"name"`
	RootPaths []string `json:"rootPaths"`
}

type codexAppStateCache struct {
	stamp    string
	revision uint64
	state    *codexAppState
}

var codexAppStates sync.Map // home → codexAppStateCache

func loadCodexAppState(home string) *codexAppState {
	snapshot, snapshotErr := codexDesktopProjectSnapshot(home)
	if v, ok := codexAppStates.Load(home); ok {
		c := v.(codexAppStateCache)
		if snapshotErr != nil || c.stamp == snapshot.Stamp && c.revision == snapshot.Revision {
			return c.state
		}
	}
	if snapshot.Projects == nil {
		return nil
	}
	raw, ok := codex.ReadDesktopSidebar(filepath.Join(home, ".codex-global-state.json"))
	if !ok {
		return nil
	}
	s := &codexAppState{unread: raw.Unread, projectless: map[string]bool{}, assigned: map[string]string{}, projects: snapshot.Projects, order: snapshot.Order}
	for _, id := range raw.Projectless {
		s.projectless[id] = true
	}
	for id, a := range raw.Assignments {
		pid := a
		if canonical := snapshot.NativeProjectIDs[pid]; canonical != "" {
			pid = canonical
		}
		s.assigned[id] = pid
	}
	for id, pid := range snapshot.ThreadProjects {
		if pid == "" {
			delete(s.assigned, id)
			s.projectless[id] = true
			continue
		}
		s.assigned[id] = pid
		delete(s.projectless, id)
	}
	add := func(id string) {
		if validID(id) && len(s.sidebar) < codexAppSidebarMax {
			s.sidebar = append(s.sidebar, id)
		}
	}
	for id := range s.projectless {
		add(id)
	}
	for id := range s.assigned {
		add(id)
	}
	slices.Sort(s.sidebar)
	s.sidebar = slices.Compact(s.sidebar)
	codexAppStates.Store(home, codexAppStateCache{stamp: snapshot.Stamp, revision: snapshot.Revision, state: s})
	return s
}

// project: the App project a thread is listed under — the one it was put in, else the one whose root holds its cwd.
func (a *codexAppState) project(thread, cwd string) (id, name string) {
	if a == nil {
		return "", ""
	}
	if pid := a.assigned[thread]; pid != "" {
		if p, ok := a.projects[pid]; ok && p.Name != "" {
			return pid, p.Name
		}
	}
	best := ""
	for pid, p := range a.projects {
		for _, root := range p.RootPaths {
			root = filepath.Clean(root)
			if pathWithin(cwd, root) && len(root) > len(best) {
				best, id, name = root, pid, p.Name
			}
		}
	}
	return id, name
}

// folderName: the App's name for the project whose folder is root (a real path, as codexProjectRoot gives a
// repository's root); "" when no project of the App has that folder. The App's sidebar shows a project by the name it
// keeps, which need not be the folder's: a project the user renamed, a ChatGPT project the App keeps in
// <CODEX_HOME>/.chatgpt-projects/g-p-<id>. A folder has one name whichever chat asks: of two projects with the same
// folder, the name that sorts first.
func (a *codexAppState) folderName(root string) string {
	if a == nil || root == "" {
		return ""
	}
	a.folders()
	return a.names[folderKey(root)]
}

// folderPlace: where the App's sidebar lists the project whose folder is root (as for folderName), from 1 down its
// project-order; 0 when no project with that folder is in the order (the order lists the projects the App's own
// sidebar shows; a ChatGPT project is not among them). Of two projects with the same folder, the first in the order.
func (a *codexAppState) folderPlace(root string) int {
	if a == nil || root == "" {
		return 0
	}
	a.folders()
	return a.places[folderKey(root)]
}

// folders reads the projects' folders once: real paths, as codexProjectRoot gives a repository's root.
func (a *codexAppState) folders() {
	a.namesOnce.Do(func() {
		a.names, a.places = map[string]string{}, map[string]int{}
		place := map[string]int{}
		for i, id := range a.order {
			if _, ok := place[id]; !ok {
				place[id] = i + 1
			}
		}
		for id, p := range a.projects {
			name := strings.TrimSpace(p.Name)
			for _, r := range p.RootPaths {
				if !filepath.IsAbs(r) {
					continue
				}
				k := folderKey(realPath(filepath.Clean(r)))
				if old, ok := a.names[k]; name != "" && (!ok || name < old) {
					a.names[k] = name
				}
				if i := place[id]; i > 0 && (a.places[k] == 0 || i < a.places[k]) {
					a.places[k] = i
				}
			}
		}
	})
}

// inNoProject: the App keeps chat thread in no project (its sidebar's chats outside every project).
func (a *codexAppState) inNoProject(thread string) bool { return a != nil && a.projectless[thread] }

// listed: the thread is one of the App's sidebar chats (put in a project, or a chat without one). Other tools that
// run the App's own codex binary write "Codex Desktop" rollouts too; the App does not list those.
func (a *codexAppState) listed(thread string) bool {
	return a != nil && (a.assigned[thread] != "" || a.projectless[thread])
}

// placeCodexDesktopIn is placeCodexDesktop with the App's state looked up once per home in a list pass (codexListPass).
func (n *Node) placeCodexDesktopIn(s *proto.Session, src codexSource, group string, subagent bool, pass *codexListPass) {
	app := pass.appState(src.home)
	s.SpaceKind = "codex_app"
	if pid, name := app.project(group, s.Cwd); pid != "" && name != "" && (app == nil || !app.projectless[group]) {
		s.SpaceID, s.SpaceName = "codex-app:"+pid, name
	} else {
		s.SpaceID, s.SpaceName = "codex-app:chats", "Codex 对话"
	}
	if src.path == "" || s.UpdatedAt.IsZero() || time.Since(s.UpdatedAt) >= codexDesktopRecent {
		return
	}
	s.Status = n.codexDesktopTurn(src.path)
	if s.Status == proto.StatusDone && (subagent || app == nil || !app.unread[src.thread]) {
		s.Status = proto.StatusIdle // looked at in the App
	}
	if s.Status == proto.StatusWorking && !subagent && s.Capabilities != nil && s.Capabilities.Send {
		// Stopped from the phone as with the App's stop button (interruptCodexDesktop); a subagent stops with its chat.
		s.Capabilities.Interrupt = true
	}
}

// slotCodexDesktop numbers each project's threads newest first (thread ids are time-ordered), as the App lists them.
func slotCodexDesktop(list []proto.Session) {
	next := map[string]int{}
	for i := len(list) - 1; i >= 0; i-- { // list is sorted by session id, oldest first
		s := &list[i]
		if s.SpaceKind != "codex_app" {
			continue
		}
		s.SlotIndex = int32(next[s.SpaceID])
		next[s.SpaceID]++
	}
}

type codexTurnMark struct {
	size   int64
	status string
	offset int64
	mtime  time.Time
}

// codexDesktopTurn follows turn boundaries across the entire rollout. After the first read, only appended
// complete lines are read: a long running turn must not lose its start when it leaves a bounded tail window.
func (n *Node) codexDesktopTurn(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return proto.StatusUnknown
	}
	status := proto.StatusDone
	var off int64
	if v, ok := codexTurnMarks.Load(path); ok {
		c := v.(codexTurnMark)
		if c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
			return c.status
		}
		if st.Size() > c.size {
			status, off = c.status, c.offset
		}
	}
	status, off, err = codex.ReadTurnStatus(path, status, off, st.Size())
	if err != nil {
		return proto.StatusUnknown
	}
	codexTurnMarks.Store(path, codexTurnMark{size: st.Size(), status: status, offset: off, mtime: st.ModTime()})
	return status
}

var codexTurnMarks sync.Map

var codexOrigins sync.Map // path → codexOrigin (the first line never changes)

func readCodexOrigin(path string) codex.Origin {
	if v, ok := codexOrigins.Load(path); ok {
		return v.(codex.Origin)
	}
	o, ok := codex.ReadOrigin(path)
	if ok {
		codexOrigins.Store(path, o)
	}
	return o
}

// codexThreadOrigin is where src's thread came from: its rollout's first line, or, once the App moved the rollout into
// its database and deleted it, the database row (codexAppThread.origin, #8191). Without either it says nothing.
func codexThreadOrigin(src codexSource) codex.Origin {
	if t, ok := codexAppThreadOf(src); ok {
		return t.Origin()
	}
	return readCodexOrigin(src.path)
}

// codexDesktopChat identifies an App chat independently of whether the App still exposes its control bus.
// An unavailable bus still leaves this a Desktop chat. The separate release-only resume path checks native ownership.
func codexDesktopChat(src codexSource) bool { return codexDesktopChatIn(src, nil) }

// codexDesktopChatIn is codexDesktopChat with sidebar state read once per home in a list pass (codexListPass).
func codexDesktopChatIn(src codexSource, pass *codexListPass) bool {
	if src.path == "" { // its rollout is gone: one the App's sidebar lists and its database has (codex_appdb.go), not a subagent
		t, ok := codexAppThreadOf(src)
		return ok && !t.Origin().Subagent
	}
	o := readCodexOrigin(src.path)
	return o.Desktop && o.Parent == "" && (o.Persona || pass.appState(src.home).listed(src.thread))
}

// loadedCodexDesktopOwner maps an unavailable window to the node's session refusal.
func loadedCodexDesktopOwner(ctx context.Context, c *appserver.Desktop, thread string) (appserver.DesktopFrame, error) {
	owner, err := c.WaitOwner(ctx, thread, codexDesktopLoadWait, openCodexThread)
	if err != nil {
		return owner, refusal("codex_desktop_not_loaded", appserver.DesktopNotLoadedMessage, nil)
	}
	return owner, nil
}

func folderKey(p string) string        { return pathutil.FolderKey(p) }
func pathWithin(dir, root string) bool { return pathutil.WithinFolder(dir, root) }
