package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// undoAutoLink takes back only what the automatic link itself made (#2704).
func TestUndoAutoLink(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	later := t0.Add(time.Second)
	key := [2]string{"m", "s"}
	item := pendingAutoLink{todoID: "big", machineID: "m", sessionID: "s", linkedAt: t0}
	agent := func(at time.Time) TodoAgent {
		return TodoAgent{MachineID: "m", SessionID: "s", LinkedAt: at, Source: "chat"}
	}
	for _, c := range []struct {
		name        string
		big, small  []TodoAgent
		active      string
		wantChanged bool
		wantBig     int
		wantActive  string
	}{
		// The store failed or refused it, nothing happened since: entry and active todo go.
		{name: "failed", big: []TodoAgent{agent(t0)}, active: "big", wantChanged: true, wantBig: 0, wantActive: ""},
		// Linked again by hand to the same todo before the write: that link (another LinkedAt) and its active todo stay.
		{name: "relinked by hand", big: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: later, Source: "manual"}}, active: "big",
			wantChanged: false, wantBig: 1, wantActive: "big"},
		// Moved by hand to another todo: the released entry the store never had goes; the user's todo stays active.
		{name: "moved by hand", big: []TodoAgent{func() TodoAgent { a := agent(t0); a.ReleasedAt = &later; return a }()},
			small: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: later, Source: "manual"}}, active: "small",
			wantChanged: true, wantBig: 0, wantActive: "small"},
		// Dismissed by the user: kept, so it is not linked again (the unlink cleared the active todo).
		{name: "dismissed", big: []TodoAgent{func() TodoAgent { a := agent(t0); a.ReleasedAt, a.DismissedAt = &later, &later; return a }()},
			wantChanged: false, wantBig: 1, wantActive: ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &todoBook{todos: map[string]*Todo{"big": {ID: "big", Agents: c.big}, "small": {ID: "small", Agents: c.small}},
				activeBySession: map[[2]string]string{}}
			if c.active != "" {
				b.activeBySession[key] = c.active
			}
			if got := b.undoAutoLink(item, later); got != c.wantChanged {
				t.Fatalf("changed = %v", got)
			}
			if len(b.todos["big"].Agents) != c.wantBig || b.activeBySession[key] != c.wantActive || len(b.todos["small"].Agents) != len(c.small) {
				t.Fatalf("big %+v, small %+v, active %q", b.todos["big"].Agents, b.todos["small"].Agents, b.activeBySession[key])
			}
		})
	}
}

// slowAutoLink is the database with its automatic links held until release, so a test can act while one is on its
// way to the store.
type slowAutoLink struct {
	*db.DB
	started  chan struct{}
	release  chan struct{}
	finished chan bool // whether the held link was inserted
}

func (s *slowAutoLink) AutoLinkUnlinkedTodoAgent(ctx context.Context, todoID, machineID, sessionID, title, cwd, agent, startError, source, via string, linkedAt time.Time) (bool, []string, error) {
	s.started <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
	inserted, released, err := s.DB.AutoLinkUnlinkedTodoAgent(ctx, todoID, machineID, sessionID, title, cwd, agent, startError, source, via, linkedAt)
	s.finished <- inserted
	return inserted, released, err
}

// An automatic link the store did not keep gives back the done todo's link it released, unless the agent was linked
// by hand since (that link released it in the store too); a todo reopened meanwhile has the agent working for it again
// (#2904).
func TestUndoAutoLinkGivesBackTheDoneTodo(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	before, later := t0.Add(-time.Hour), t0.Add(time.Second)
	key := [2]string{"m", "s"}
	item := pendingAutoLink{todoID: "big", machineID: "m", sessionID: "s", linkedAt: t0, released: []string{"old"}}
	for _, c := range []struct {
		name         string
		hand, reopen bool
		wantReleased bool
		wantActive   string
	}{
		{name: "refused", wantActive: ""},
		{name: "linked by hand since", hand: true, wantReleased: true, wantActive: "other"},
		{name: "reopened meanwhile", reopen: true, wantActive: "old"},
	} {
		t.Run(c.name, func(t *testing.T) {
			old := &Todo{ID: "old", DoneAt: &before, Agents: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: before, ReleasedAt: &t0, Source: "manual"}}}
			if c.reopen {
				old.DoneAt = nil
			}
			b := &todoBook{todos: map[string]*Todo{"old": old, "other": {ID: "other"},
				"big": {ID: "big", Agents: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: t0, Source: "space"}}}},
				activeBySession: map[[2]string]string{key: "big"}}
			if c.hand {
				b.todos["big"].Agents[0].ReleasedAt = &later
				b.todos["other"].Agents = []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: later, Source: "manual"}}
				b.activeBySession[key] = "other"
			}
			if !b.undoAutoLink(item, later) {
				t.Fatal("nothing changed")
			}
			if released := old.Agents[0].ReleasedAt != nil; released != c.wantReleased || b.activeBySession[key] != c.wantActive || len(b.todos["big"].Agents) != 0 {
				t.Fatalf("old %+v, active %q, big %+v", old.Agents[0], b.activeBySession[key], b.todos["big"].Agents)
			}
		})
	}
}
