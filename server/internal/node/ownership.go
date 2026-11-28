package node

import (
	"slices"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Ownership (#2576): the same Claude session can be live in two hosts at once — resumed in herdr while a botmux session
// still runs it, say. A session is listed once, in the host that owns it; every send, key and interrupt goes there
// (they route by the listed pane), and its state comes from there. The owner, in order:
//
//  1. the pane whose own claude process runs the session (~/.claude/sessions/<pid>.json, sessionFromProcess);
//  2. else the pane whose SessionStart hook named the session last;
//  3. else the pane in herdr;
//  4. else the lowest pane id (stable across refreshes).
//
// Two live panes of the same host keep the behaviour from before hosts could be combined (the later one listed).

// ownership is what the rules remember between refreshes.
type ownership struct {
	mu        sync.Mutex
	starts    map[string]paneStart // pane ref → the session its last SessionStart hook named, and when
	ownerKind map[string]string    // session id → the host kind that owns it (the last refresh), for tests and logs
}

type paneStart struct {
	session string
	at      time.Time
}

// noteSessionStart remembers a SessionStart hook from a pane (rule 2).
func (n *Node) noteSessionStart(pane, id string) {
	if pane == "" || !validID(id) {
		return
	}
	n.owners.mu.Lock()
	defer n.owners.mu.Unlock()
	if n.owners.starts == nil {
		n.owners.starts = map[string]paneStart{}
	}
	n.owners.starts[pane] = paneStart{session: id, at: time.Now()}
}

// startedAt is when pane's last SessionStart hook named session id (zero: it did not, or never said).
func (n *Node) startedAt(pane, id string) time.Time {
	n.owners.mu.Lock()
	defer n.owners.mu.Unlock()
	if st, ok := n.owners.starts[pane]; ok && st.session == id {
		return st.at
	}
	return time.Time{}
}

// contest is the live panes of other hosts that list the same session (setAgentsAt); resolved outside n.mu.
type contest map[string][]host.Agent

// add notes that a and b, both live in different hosts, list session id.
func (c contest) add(id string, a, b host.Agent) {
	for _, x := range []host.Agent{a, b} {
		if !slices.ContainsFunc(c[id], func(y host.Agent) bool { return y.PaneID == x.PaneID }) {
			c[id] = append(c[id], x)
		}
	}
}

// crossHost: a running agent of another host already lists session id in m (the case ownership decides).
func crossHost(m map[string]host.Agent, id string, a host.Agent) bool {
	prev, ok := m[id]
	return ok && prev.AgentStatus != proto.StatusOffline && a.AgentStatus != proto.StatusOffline &&
		host.ParsePane(prev.PaneID).Kind != host.ParsePane(a.PaneID).Kind
}

// resolve settles every contested session in m (the owner listed, the others left out). Called without n.mu: rule 1
// asks the panes' processes, every candidate's host even in a refresh of another host (unlike the rest of
// setAgentsAt, #5816): a contest is rare, and its owner must not depend on which host refreshed last.
func (n *Node) resolve(m map[string]host.Agent, c contest) {
	owners := map[string]string{}
	for id, cands := range c {
		owner := n.ownerOf(id, cands)
		m[id] = owner
		owners[id] = host.ParsePane(owner.PaneID).Kind
	}
	n.owners.mu.Lock()
	prev := n.owners.ownerKind
	n.owners.ownerKind = owners
	n.owners.mu.Unlock()
	for id, k := range owners {
		if prev[id] != k {
			n.logf("session %s: live in several hosts, owned by %s", id, k)
		}
	}
}

// ownerOf picks the owner among the live panes listing session id (the rules above).
func (n *Node) ownerOf(id string, cands []host.Agent) host.Agent {
	// 1. the pane whose own claude runs it
	var byProcess []host.Agent
	for _, a := range cands {
		if n.sessionFromProcess(a.PaneID) == id {
			byProcess = append(byProcess, a)
		}
	}
	if len(byProcess) == 1 {
		return byProcess[0]
	}
	if len(byProcess) > 1 {
		cands = byProcess
	}
	// 2. the last SessionStart hook naming it
	var latest []host.Agent
	var at time.Time
	for _, a := range cands {
		t := n.startedAt(a.PaneID, id)
		switch {
		case t.IsZero():
		case t.After(at):
			latest, at = []host.Agent{a}, t
		case t.Equal(at):
			latest = append(latest, a)
		}
	}
	if len(latest) == 1 {
		return latest[0]
	}
	if len(latest) > 1 {
		cands = latest
	}
	// 3. herdr
	var inHerdr []host.Agent
	for _, a := range cands {
		if host.ParsePane(a.PaneID).Kind == host.Herdr {
			inHerdr = append(inHerdr, a)
		}
	}
	if len(inHerdr) >= 1 {
		cands = inHerdr
	}
	// 4. stable order
	return slices.MinFunc(cands, func(a, b host.Agent) int {
		switch {
		case a.PaneID < b.PaneID:
			return -1
		case a.PaneID > b.PaneID:
			return 1
		}
		return 0
	})
}
