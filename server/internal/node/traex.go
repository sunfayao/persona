package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// A Codex-compatible CLI can run in herdr panes without being recognized by the host. The node therefore finds
// it itself in every host's panes:
//   - <CLI data>/thread-writer-locks/<thread id>.owner.json names the pid writing each thread (session);
//   - that pid's environment holds the host's pane variable (host.PaneEnv: HERDR_PANE_ID), so pane → session is
//     exact, no guessing by directory;
//   - its conversation is a rollout under <CLI data>/sessions/YYYY/MM/DD, the newer Codex format (transcript/codex.go);
//   - herdr has no state for the pane: a spinner in the terminal title or "esc to interrupt" on screen is working,
//     a numbered choice on screen is blocked, anything else is a finished turn (the hub turns that into "Unread" / "Seen").

const traexAgent = "traex"

func (n *Node) traexHome() string {
	if n.sessionScope != nil {
		return filepath.Join(n.dir, "traex")
	}
	return filepath.Join(n.home, ".trae", "cli")
}

func (n *Node) traexPath(id string) string {
	if _, ok := n.traexThread(id); ok {
		p := n.codexAppHistoryFile(codexSource{home: n.traexHome(), thread: id})
		if fileExists(p) {
			return p
		}
	}
	return n.traexRolloutPath(id)
}

// traexRolloutPath returns the native JSONL rollout for TRAE thread id when present on disk:
// threads.rollout_path from state_*.sqlite first, then rolloutPath under ~/.trae/cli/sessions.
func (n *Node) traexRolloutPath(id string) string {
	if t, ok := n.traexThread(id); ok && t.RolloutPath != "" && fileExists(t.RolloutPath) {
		return t.RolloutPath
	}
	return rolloutPath(n.traexHome(), id)
}

// traexEnrichMeta merges rollout-only telemetry (token_count context tokens/window/auto-compact,
// output tokens, reasoning effort, context stats, version) onto quickMeta read from synthesized
// paginated history, which contains chat items without turn_context or token_count events.
func (n *Node) traexEnrichMeta(id, path string, m quickMeta) quickMeta {
	rp := n.traexRolloutPath(id)
	if rp == "" || rp == path {
		return m
	}
	st, err := os.Stat(rp)
	if err != nil {
		return m
	}
	rm := n.meta(rp, st)
	if m.contextTokens == 0 && rm.contextTokens > 0 {
		m.contextTokens = rm.contextTokens
		if m.contextWindow == 0 {
			m.contextWindow = rm.contextWindow
		}
		if m.autoCompactThreshold == 0 {
			m.autoCompactThreshold = rm.autoCompactThreshold
		}
	}
	if m.outputTokens == 0 && rm.outputTokens > 0 {
		m.outputTokens = rm.outputTokens
	}
	if !rm.contextStats.Empty() {
		m.contextStats = rm.contextStats
		m.contextFullWindow = rm.contextFullWindow
	}
	if m.effort == "" && rm.effort != "" {
		m.effort, m.effortAt = rm.effort, rm.effortAt
	}
	if m.model == "" && rm.model != "" {
		m.model, m.modelAt = rm.model, rm.modelAt
	}
	if m.version == "" && rm.version != "" {
		m.version, m.versionAt = rm.version, rm.versionAt
	}
	return m
}

var traexModelWindowCache = struct {
	sync.Mutex
	byID map[string]int
}{byID: map[string]int{}}

func splitTraexEffortModel(id string) (base, effort string, ok bool) {
	b, suf, found := strings.Cut(strings.TrimSpace(id), "@")
	if !found || b == "" {
		return "", "", false
	}
	lower := strings.ToLower(strings.TrimSpace(suf))
	for _, lvl := range proto.EffortLevels {
		if lower == lvl {
			return b, lvl, true
		}
	}
	return "", "", false
}

func recordTraexModelWindows(windows map[string]int) {
	if len(windows) == 0 {
		return
	}
	traexModelWindowCache.Lock()
	defer traexModelWindowCache.Unlock()
	for id, w := range windows {
		key := strings.ToLower(strings.TrimSpace(id))
		if key != "" && w > 0 {
			traexModelWindowCache.byID[key] = w
		}
	}
}

func lookupTraexModelWindow(models ...string) int {
	traexModelWindowCache.Lock()
	defer traexModelWindowCache.Unlock()
	if len(traexModelWindowCache.byID) == 0 {
		return 0
	}
	for _, raw := range models {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" {
			continue
		}
		if w := traexModelWindowCache.byID[key]; w > 0 {
			return w
		}
		if base, _, ok := splitTraexEffortModel(key); ok {
			if w := traexModelWindowCache.byID[base]; w > 0 {
				return w
			}
		}
	}
	return 0
}

func (n *Node) traexTitle(id string) string {
	if t, ok := n.traexThread(id); ok && t.Title != "" {
		return t.Title
	}
	return threadTitle(n.traexHome(), id)
}

var traexCache = struct {
	sync.Mutex
	panes  map[int]string // pid → its pane, a stored PaneRef (from its environment; a pid never moves)
	status map[string]traexState
}{panes: map[int]string{}, status: map[string]traexState{}}

type traexState struct {
	session, status string
	at              time.Time
	queued          []string
	queueRead       bool
}

// Fresh starts need positive writer evidence, including when old IDs were archived,
// outside the rollout scan window, or absent from a cached metadata list.
func (n *Node) traexLiveThread(pane, preferred string) string {
	owners := map[int][]string{}
	for pid, ids := range n.traexOwnerThreads(codexPaginatedThreads(n.traexHome(), true)) {
		if n.paneOfPID(pid) == pane {
			owners[pid] = ids
		}
	}
	return traexPaneThread(owners, preferred, parentPID)
}

// No foreground match does not mean the native writer is gone. TRAE can keep another thread locked in the same TUI.
func (n *Node) traexWriterOwns(id string) bool {
	for _, ids := range n.traexOwnerThreads(codexPaginatedThreads(n.traexHome(), true)) {
		for _, owned := range ids {
			if owned == id {
				return true
			}
		}
	}
	return false
}

// traexOwnerThreads keeps every main writer owned by a process, newest first: a host's existing session may still
// have its own writer even when another thread is newer. Known subagents never fall back to their own id. Legacy
// subagents may supply a root confirmed by rollout metadata; older CLIs without metadata keep the owner-file fallback.
func (n *Node) traexOwnerThreads(threads map[string]codexAppThread) map[int][]string {
	dir := filepath.Join(n.traexHome(), "thread-writer-locks")
	files, _ := filepath.Glob(filepath.Join(dir, "*.owner.json"))
	type owner struct {
		thread string
		mtime  time.Time
	}
	byPID := map[int][]owner{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var o struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(b, &o) != nil || o.PID <= 0 || !alive(o.PID) {
			continue
		}
		thread := strings.TrimSuffix(filepath.Base(f), ".owner.json")
		if validID(thread) {
			byPID[o.PID] = append(byPID[o.PID], owner{thread, st.ModTime()})
		}
	}
	home := n.traexHome()
	paginated := codexHistoryDBPath(home) != ""
	out := make(map[int][]string, len(byPID))
	for pid, owners := range byPID {
		sort.Slice(owners, func(i, j int) bool {
			// The TUI can hold more than one thread lock. Looking at a referenced thread can
			// refresh its lock without writing a turn to it. Prefer native conversation activity.
			left, right := threads[owners[i].thread].UpdatedAtMs, threads[owners[j].thread].UpdatedAtMs
			if left != right {
				return left > right
			}
			if owners[i].mtime.Equal(owners[j].mtime) {
				return owners[i].thread < owners[j].thread
			}
			return owners[i].mtime.After(owners[j].mtime)
		})
		var main, fallback []string
		for _, o := range owners {
			t, known := threads[o.thread]
			if known && traexConversation(t) {
				main = append(main, o.thread)
				continue
			}
			p := rolloutPath(home, o.thread)
			if p == "" {
				// A new paginated owner can precede its state row. Wait for its source.
				if !known && !paginated {
					fallback = append(fallback, o.thread)
				}
				continue
			}
			m := metaOf(home, p)
			if !known && !m.sub {
				main = append(main, o.thread)
				continue
			}
			root := m.root
			if root == o.thread || !validID(root) {
				continue
			}
			parent, listed := threads[root]
			if listed && !traexConversation(parent) {
				continue
			}
			rootPath := rolloutPath(home, root)
			if (listed || rootPath != "") && (rootPath == "" || !metaOf(home, rootPath).sub) {
				fallback = append(fallback, root)
			}
		}
		if len(main) > 0 {
			out[pid] = main
		} else if len(fallback) > 0 {
			out[pid] = fallback
		}
	}
	return out
}

// alive says whether the process behind a pid file this user's claude or a compatible CLI wrote still runs. A pid now held by
// another user's process is a reused one, not the program that wrote the file (#2141). The cost: a claude run as
// another user that still writes into this user's home (`sudo claude`, sudo keeping HOME on macOS) counts as gone.
func alive(pid int) bool { return osproc.AliveMine(pid) }

var herdrPaneEnvRe = osproc.PaneEnvRE(herdrPaneEnv)

// paneEnvRes caches paneEnvRe per variable.
var paneEnvRes sync.Map // name -> *regexp.Regexp

// paneOfPID reads the pane a process runs in from its environment (Linux: /proc; macOS: ps -E): the variable of the
// first host whose PaneEnv it holds, as a stored PaneRef. Cached per pid.
func (n *Node) paneOfPID(pid int) string {
	traexCache.Lock()
	p, ok := traexCache.panes[pid]
	traexCache.Unlock()
	if ok {
		return p
	}
	if len(n.hosts.All()) == 0 {
		return "" // nothing to look for; not cached, the pid's pane is not known to be none
	}
	var env []byte
	if runtime.GOOS == "linux" {
		env, _ = os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		env, _ = exec.CommandContext(ctx, "ps", "-E", "-ww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
		cancel()
	}
	for _, h := range n.hosts.All() {
		re, ok := paneEnvRes.Load(h.PaneEnv())
		if !ok {
			re, _ = paneEnvRes.LoadOrStore(h.PaneEnv(), osproc.PaneEnvRE(h.PaneEnv()))
		}
		if m := re.(*regexp.Regexp).FindSubmatch(env); m != nil {
			p = host.PaneRef{Kind: h.Kind(), ID: string(m[1])}.String()
			if h.Kind() == host.Herdr { // which server's pane: a named session's is qualified (#2196)
				picked := pickedEnv(string(env))
				p = n.envPane(func(name string) string { return picked[name] })
			}
			break
		}
	}
	traexCache.Lock()
	traexCache.panes[pid] = p
	traexCache.Unlock()
	return p
}

// traexAgents turns the panes running a compatible CLI into agents, with their session id and a state of our own.
func (n *Node) traexAgents(ctx context.Context, panes []host.Agent, agents []host.Agent) []host.Agent {
	threads := n.traexThreads()
	owners := n.traexOwnerThreads(threads)
	isAgent := map[string]bool{}
	compatible := map[string]int{}
	for i, a := range agents {
		// PaneReportResume claims a compatible-CLI pane in herdr so resume_argv persists, which makes herdr list a
		// session-less traex entry in snap.Agents; keep filling its session id and screen status from the owner file.
		if a.Agent != traexAgent {
			isAgent[a.PaneID] = true
		} else {
			compatible[a.PaneID] = i
			if t, known := threads[a.SessionID()]; known && !traexConversation(t) {
				agents[i].AgentSession = nil // a stale host claim cannot promote a known guardian to a terminal chat
			}
		}
	}
	byPane := map[string]map[int][]string{}
	for pid, ids := range owners {
		pane := n.paneOfPID(pid)
		if byPane[pane] == nil {
			byPane[pane] = map[int][]string{}
		}
		byPane[pane][pid] = ids
	}
	var out []host.Agent
	for _, p := range panes {
		pane := p.PaneID
		if isAgent[pane] || len(byPane[pane]) == 0 {
			if i, ok := compatible[pane]; ok && len(byPane[pane]) == 0 {
				agents[i].AgentSession = nil
			}
			continue
		}
		preferred := ""
		if i, ok := compatible[pane]; ok {
			preferred = agents[i].SessionID()
		}
		thread := traexPaneThread(byPane[pane], preferred, parentPID)
		if thread == "" {
			continue
		}
		p.Agent = traexAgent
		p.AgentSession = &host.AgentSession{Kind: "id", Value: thread}
		// A host workspace is a terminal container, which may be unrelated to where
		// Run started this CLI. Keep its placement, but report the conversation's cwd.
		if t, ok := threads[thread]; ok && t.Cwd != "" {
			p.Cwd = t.Cwd
		} else if cwd := metaOf(n.traexHome(), rolloutPath(n.traexHome(), thread)).cwd; cwd != "" {
			p.Cwd = cwd
		}
		p.AgentStatus = n.traexStatus(ctx, p)
		if i, ok := compatible[pane]; ok {
			agents[i] = p
			continue
		}
		out = append(out, p)
	}
	return out
}

// traexPaneThread picks one terminal conversation per pane. Nested exec writers inherit the pane environment,
// but cannot replace an ancestor's conversation. Among independent roots a still-owned host id wins, else the
// lowest pid makes the choice stable. Without ancestry and a host claim, ambiguous writers are left unclaimed.
func traexPaneThread(owners map[int][]string, preferred string, parent func(int) (int, error)) string {
	pids := make([]int, 0, len(owners))
	for pid := range owners {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	roots := pids
	complete := true
	if len(pids) > 1 { // most panes have one writer process: no process-tree reads needed
		roots = nil
		for _, pid := range pids {
			ancestors := lineage(pid, func(pid int) (int, error) {
				next, err := parent(pid)
				if err != nil {
					complete = false
				}
				return next, err
			})
			if len(ancestors) == ancestorsMost {
				complete = false
			}
			nested := false
			for _, ancestor := range ancestors[1:] {
				if len(owners[ancestor]) > 0 {
					nested = true
					break
				}
			}
			if !nested {
				roots = append(roots, pid)
			}
		}
	}
	for _, pid := range roots {
		if owners[pid][0] == preferred {
			return preferred
		}
	}
	if len(roots) == 0 || !complete && len(roots) > 1 {
		return ""
	}
	return owners[roots[0]][0]
}

var traexChoiceRe = lazyre.New(`(?m)^\s*[›❯>]\s*1\.\s+\S`)

// traexStatus shares one screen read between status and the Herdr queue. The spinner is immediate evidence of
// work unless the most recent screen shows a blocking question. Each pane is sampled at most every two seconds.
func (n *Node) traexStatus(ctx context.Context, p host.Agent) string {
	title := []rune(strings.TrimSpace(p.RawTitle))
	spinning := len(title) > 0 && title[0] >= 0x2800 && title[0] <= 0x28FF
	traexCache.Lock()
	s, ok := traexCache.status[p.PaneID]
	traexCache.Unlock()
	if !ok || s.session != p.SessionID() {
		s = traexState{session: p.SessionID()}
	}
	if time.Since(s.at) < 2*time.Second {
		if spinning && s.status != proto.StatusBlocked {
			return proto.StatusWorking
		}
		return s.status
	}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	colored := host.ParsePane(p.PaneID).Kind == host.Herdr
	var text string
	var err error
	if colored {
		text, err = n.readPaneANSI(rctx, p.PaneID, "visible", 80)
		if errors.Is(err, herdr.ErrTruncated) {
			colored = false
			text, err = n.readPane(rctx, p.PaneID, "visible", 40)
		}
	} else {
		text, err = n.readPane(rctx, p.PaneID, "visible", 40)
	}
	plain := screen.Plain(text)
	status := proto.StatusDone
	switch {
	case err != nil:
		status = s.status // keep the last known
		if spinning && status != proto.StatusBlocked {
			status = proto.StatusWorking
		} else if status == "" {
			status = proto.StatusUnknown
		}
	case screen.ReadTraeQuestion(plain) != nil, traexChoiceRe.MatchString(plain):
		status = proto.StatusBlocked
	case spinning, strings.Contains(plain, "esc to interrupt"):
		status = proto.StatusWorking
	}
	s = traexState{session: p.SessionID(), status: status, at: time.Now()}
	if colored && err == nil && status == proto.StatusWorking {
		s.queued, s.queueRead = parseTraeComposer(text).queued, true
	}
	traexCache.Lock()
	traexCache.status[p.PaneID] = s
	traexCache.Unlock()
	return status
}

func traexQueue(pane, session string) ([]string, bool) {
	traexCache.Lock()
	defer traexCache.Unlock()
	s := traexCache.status[pane]
	return s.queued, s.session == session && s.queueRead && time.Since(s.at) < 2*time.Second
}
