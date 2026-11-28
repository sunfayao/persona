package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// ownHost is a host for the ownership tests: its agents (or an error), the pids in each pane, and what it was sent.
type ownHost struct {
	bareHost
	mu      sync.Mutex
	agents  []host.Agent
	err     error
	pids    map[string]int // pane ref → the claude's pid
	sent    []string       // pane ref + ": " + text
	calls   int
	looks   int   // PaneProcesses calls
	lookErr error // what PaneProcesses fails with, when set
}

func newOwnHost(kind string, agents ...host.Agent) *ownHost {
	return &ownHost{bareHost: bareHost{kind: kind}, agents: agents, pids: map[string]int{}}
}

func (h *ownHost) Snapshot(context.Context) (*host.Layout, error) {
	return nil, errors.New("no snapshot") // lookAt falls back to Agents
}

func (h *ownHost) Agents(context.Context) ([]host.Agent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.err != nil {
		return nil, h.err
	}
	return slices.Clone(h.agents), nil
}

func (h *ownHost) PaneProcesses(_ context.Context, pane host.PaneRef) ([]int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.looks++
	if h.lookErr != nil {
		return nil, h.lookErr
	}
	if pid := h.pids[pane.String()]; pid != 0 {
		return []int{pid}, nil
	}
	return nil, nil
}

func (h *ownHost) SendText(_ context.Context, pane host.PaneRef, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, pane.String()+": "+text)
	return nil
}

func (h *ownHost) looked() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.looks
}

func (h *ownHost) sentText() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.sent)
}

const (
	sidShared  = "0d000000-0000-4000-8000-000000000001" // live in a herdr pane and a botmux session at once
	sidOnly    = "0d000000-0000-4000-8000-000000000002" // live in the botmux session alone
	herdrPane  = "w1:p1"
	botmuxPane = "botmux:b1"
)

func claudeIn(pane, id, status string) host.Agent {
	return host.Agent{Agent: "claude", AgentSession: &host.AgentSession{Kind: "id", Value: id}, AgentStatus: status,
		PaneID: pane, Cwd: "/work/repo"}
}

// ownNode is a node driving herdr and botmux through two ownHosts.
func ownNode(t *testing.T) (*Node, *ownHost, *ownHost, string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	n, home := testNode(t)
	hh := newOwnHost(host.Herdr, claudeIn(herdrPane, sidShared, proto.StatusWorking))
	bh := newOwnHost(host.Botmux, claudeIn(botmuxPane, sidShared, proto.StatusIdle), claudeIn("botmux:b2", sidOnly, proto.StatusIdle))
	n.hosts, n.panes, n.panePIDs = host.NewRegistry(hh, bh), map[string]string{}, map[string]panePID{}
	return n, hh, bh, home
}

// writeClaudePID writes ~/.claude/sessions/<pid>.json naming the session a claude process runs.
func writeClaudePID(t *testing.T, home string, pid int, id string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id})
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// dueLooks makes the process lookups of kind's panes due (their pids were looked up over a minute ago).
func dueLooks(n *Node, kind string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for p, c := range n.panePIDs {
		if host.ParsePane(p).Kind == kind {
			c.checked = c.checked.Add(-2 * time.Minute)
			n.panePIDs[p] = c
		}
	}
}

// Rule 3: two live panes, neither one's process or hook says more: herdr's pane owns the session, which is listed once.
func TestOwnerIsTheHerdrPaneAtLast(t *testing.T) {
	n, _, _, _ := ownNode(t)
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != herdrPane {
		t.Errorf("owner %q, want the herdr pane", a.PaneID)
	}
	if a, _ := n.agent(sidOnly); a.PaneID != "botmux:b2" {
		t.Errorf("botmux's own session: %q", a.PaneID)
	}
}

// Rule 2 beats rule 3: the pane whose SessionStart hook named the session last owns it.
func TestOwnerIsTheLastSessionStart(t *testing.T) {
	n, _, _, _ := ownNode(t)
	n.noteSessionStart(herdrPane, sidShared)
	time.Sleep(2 * time.Millisecond)
	n.noteSessionStart(botmuxPane, sidShared) // resumed in botmux after herdr
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != botmuxPane {
		t.Errorf("owner %q, want the pane that started it last", a.PaneID)
	}
	// A SessionStart that named another session (the pane moved on) does not count.
	n.noteSessionStart(botmuxPane, sidOnly)
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != herdrPane {
		t.Errorf("owner %q after the botmux session started another session", a.PaneID)
	}
}

// Rule 1 beats both: the pane whose own claude process runs the session.
func TestOwnerIsThePaneWhoseProcessRunsIt(t *testing.T) {
	n, _, bh, home := ownNode(t)
	n.noteSessionStart(herdrPane, sidShared) // the hook and herdr both say herdr
	bh.pids[botmuxPane] = 5151
	writeClaudePID(t, home, 5151, sidShared)
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != botmuxPane {
		t.Errorf("owner %q, want the pane whose claude runs it", a.PaneID)
	}
}

// Sends and pane lookups go to the owner only; the other host gets nothing.
func TestSendGoesToTheOwnerOnly(t *testing.T) {
	n, hh, bh, _ := ownNode(t)
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	pane, err := n.paneFor(context.Background(), sidShared)
	if err != nil || pane != herdrPane {
		t.Fatalf("paneFor = %q, %v", pane, err)
	}
	if err := n.sendText(context.Background(), pane, "hello"); err != nil {
		t.Fatal(err)
	}
	if got := hh.sentText(); !slices.Equal(got, []string{herdrPane + ": hello"}) {
		t.Errorf("herdr got %v", got)
	}
	if got := bh.sentText(); len(got) != 0 {
		t.Errorf("botmux got %v", got)
	}
}

// claudeOutsidePanes is a claude in a plain terminal (this test's own pid, so it is alive) running a session no pane
// runs: the merge then also checks what the panes whose sessions their host names run (setAgentsAt's own).
func claudeOutsidePanes(t *testing.T, home string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sessionId": "0d000000-0000-4000-8000-0000000000ff", "kind": "interactive"})
	if err := os.WriteFile(filepath.Join(home, ".claude", "sessions", strconv.Itoa(os.Getpid())+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A refresh asks only the hosts that answered it what their panes run (#5816): another host's pane keeps the pid its
// own refresh found, and its session stays listed in that pane. Each host has a claude it names no session for and
// one it names, checked because a claude outside every pane runs another session.
func TestARefreshAsksOnlyItsOwnHostsWhatTheirPanesRun(t *testing.T) {
	const (
		herdrNamed  = "0d000000-0000-4000-8000-000000000003"
		botmuxNamed = "0d000000-0000-4000-8000-000000000004"
	)
	n, hh, bh, home := ownNode(t)
	unnamed := func(pane string) host.Agent {
		return host.Agent{Agent: "claude", PaneID: pane, Cwd: "/work/repo", AgentStatus: proto.StatusIdle}
	}
	hh.agents = []host.Agent{unnamed(herdrPane), claudeIn("w1:p2", herdrNamed, proto.StatusIdle)}
	bh.agents = []host.Agent{unnamed(botmuxPane), claudeIn("botmux:b2", botmuxNamed, proto.StatusIdle)}
	hh.pids[herdrPane], hh.pids["w1:p2"], bh.pids[botmuxPane], bh.pids["botmux:b2"] = 4101, 4102, 4201, 4202
	writeClaudePID(t, home, 4101, sidShared)
	writeClaudePID(t, home, 4102, herdrNamed)
	writeClaudePID(t, home, 4201, sidOnly)
	writeClaudePID(t, home, 4202, botmuxNamed)
	claudeOutsidePanes(t, home)
	listed := func(when string) {
		t.Helper()
		for id, pane := range map[string]string{sidShared: herdrPane, herdrNamed: "w1:p2", sidOnly: botmuxPane, botmuxNamed: "botmux:b2"} {
			if a, _ := n.agent(id); a.PaneID != pane {
				t.Errorf("%s: %s in %q, want %s", when, id, a.PaneID, pane)
			}
		}
	}
	ctx := context.Background()
	if err := n.refreshHosts(ctx); err != nil {
		t.Fatal(err)
	}
	listed("every host refreshed")
	if hh.looked() != 2 || bh.looked() != 2 {
		t.Fatalf("a refresh of every host asked herdr %d times, botmux %d times, want each pane once", hh.looked(), bh.looked())
	}
	for _, c := range []struct {
		name             string
		refreshed, other *ownHost
	}{{"botmux's refresh", bh, hh}, {"herdr's refresh", hh, bh}} {
		dueLooks(n, host.Herdr) // a minute on: every pane is due for another look
		dueLooks(n, host.Botmux)
		own, other := c.refreshed.looked(), c.other.looked()
		if err := n.refreshHosts(ctx, c.refreshed); err != nil {
			t.Fatal(err)
		}
		if got := c.other.looked() - other; got != 0 {
			t.Errorf("%s asked the other host what its panes run (%d times)", c.name, got)
		}
		if got := c.refreshed.looked() - own; got != 2 {
			t.Errorf("%s looked at its own panes %d times, want 2", c.name, got)
		}
		listed("after " + c.name)
	}
}

// A session live in two hosts has the same owner whichever host refreshed (ownership's rule 1 asks every candidate's
// host, #5816). herdr names the session in its pane and was never asked what runs there; then the same session runs
// in a botmux session too. Both panes' claudes run it and no hook said which started it last, so herdr's pane owns it
// (rule 3), after botmux's refresh as after herdr's.
func TestAContestedSessionsOwnerDoesNotDependOnWhichHostRefreshed(t *testing.T) {
	n, hh, bh, home := ownNode(t)
	hh.agents, bh.agents = []host.Agent{claudeIn(herdrPane, sidShared, proto.StatusIdle)}, nil
	hh.pids[herdrPane], bh.pids[botmuxPane] = 4101, 4201
	writeClaudePID(t, home, 4101, sidShared)
	writeClaudePID(t, home, 4201, sidShared)
	ctx := context.Background()
	if err := n.refreshHosts(ctx); err != nil {
		t.Fatal(err)
	}
	bh.agents = []host.Agent{{Agent: "claude", PaneID: botmuxPane, Cwd: "/work/repo", AgentStatus: proto.StatusIdle}}
	for _, c := range []struct {
		name string
		h    *ownHost
	}{{"botmux's refresh", bh}, {"herdr's refresh", hh}, {"botmux's next refresh", bh}} {
		if err := n.refreshHosts(ctx, c.h); err != nil {
			t.Fatal(err)
		}
		if a, _ := n.agent(sidShared); a.PaneID != herdrPane {
			t.Errorf("after %s: owner %q, want herdr's pane", c.name, a.PaneID)
		}
	}
}

// A process lookup that fails keeps the pid found before (#5816): the other host's refreshes read that pid
// (sessionFromKnownProcess), and the pane's own refresh must not drop the session they list.
func TestAFailedLookupKeepsThePidFoundBefore(t *testing.T) {
	n, hh, bh, home := ownNode(t)
	hh.agents = nil
	bh.agents = []host.Agent{{Agent: "claude", PaneID: botmuxPane, Cwd: "/work/repo", AgentStatus: proto.StatusIdle}}
	bh.pids[botmuxPane] = 4201
	writeClaudePID(t, home, 4201, sidOnly)
	ctx := context.Background()
	if err := n.refreshHosts(ctx); err != nil {
		t.Fatal(err)
	}
	bh.mu.Lock()
	bh.lookErr = errors.New("ps: interrupted")
	bh.mu.Unlock()
	for _, c := range []struct {
		name string
		h    *ownHost
	}{{"herdr's refresh", hh}, {"botmux's refresh", bh}} {
		dueLooks(n, host.Botmux)
		if err := n.refreshHosts(ctx, c.h); err != nil {
			t.Fatal(err)
		}
		if a, ok := n.agent(sidOnly); !ok || a.PaneID != botmuxPane {
			t.Errorf("after %s: listed %v in %q", c.name, ok, a.PaneID)
		}
	}
}

// Same-host duplicates keep the later pane listed; an offline row gives way to the live one.
func TestNoConflictWithinOneHostOrWithOffline(t *testing.T) {
	n, hh, bh, _ := ownNode(t)
	hh.agents = []host.Agent{claudeIn("w1:p1", sidShared, proto.StatusIdle), claudeIn("w1:p2", sidShared, proto.StatusIdle)}
	bh.agents = []host.Agent{claudeIn(botmuxPane, sidOnly, proto.StatusOffline), claudeIn("botmux:b9", sidShared, proto.StatusOffline)}
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != "w1:p2" {
		t.Errorf("same host: owner %q", a.PaneID)
	}
}

// One host down: the other's sessions are listed and driven as before; the host that came back is listed again.
func TestOneHostDownLeavesTheOther(t *testing.T) {
	n, hh, bh, _ := ownNode(t)
	hh.err = errors.New("herdr: connection refused")
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatalf("refresh with one host down: %v", err)
	}
	if a, ok := n.agent(sidOnly); !ok || a.PaneID != "botmux:b2" {
		t.Errorf("botmux's own session: %v %v", a, ok)
	}
	if a, _ := n.agent(sidShared); a.PaneID != botmuxPane {
		t.Errorf("the shared session: owner %q, want botmux (herdr gave nothing yet)", a.PaneID)
	}
	if err := n.sendText(context.Background(), "botmux:b2", "still here"); err != nil || len(bh.sentText()) != 1 {
		t.Errorf("send to botmux: %v %v", err, bh.sentText())
	}
	// herdr not running is an answer: nothing there, botmux untouched.
	hh.err = host.ErrNotRunning
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidOnly); !ok {
		t.Error("botmux's session went with herdr")
	}
	hh.err = nil
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, _ := n.agent(sidShared); a.PaneID != herdrPane {
		t.Errorf("herdr back: owner %q", a.PaneID)
	}
}

// Pane-less operations go to herdr.
func TestPaneLessOperationsGoToHerdr(t *testing.T) {
	n, hh, _, _ := ownNode(t)
	if h, _ := n.hostOfKind(""); h != hh {
		t.Errorf("hostOfKind default = %v", h)
	}
}
