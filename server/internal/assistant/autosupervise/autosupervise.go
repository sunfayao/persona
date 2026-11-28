// Package autosupervise is the bookkeeping of the supervisor Agent the hub attaches to a dispatched request by itself
// (the setting assistant.todo_supervisor): which starts are on their way, which roots just failed to get one and wait
// out their cooldown, which conversation is above a supervisor, and how many of a root's pieces a supervisor can
// take. It is pure apart from the Gate's own memory and a clock the caller injects: starting the Agent, linking it and
// moving the ledgers are the assistant root's.
package autosupervise

import (
	"sync"
	"time"
)

// The values of the setting assistant.todo_supervisor.
const (
	Auto     = "auto"
	Explicit = "explicit"
)

// Value is the setting as the owner sees it: unset is Auto.
func Value(setting string) string {
	if setting == "" {
		return Auto
	}
	return setting
}

// On says the hub attaches supervisors by itself under setting ("" is Auto: only Explicit turns it off).
func On(setting string) bool { return setting != Explicit }

// Cooldown is how long a root whose supervisor could not be started (or reached) goes without a new try: its signals
// go to the main conversation meanwhile.
const Cooldown = 30 * time.Minute

// Clock is the time the Gate counts the cooldown by.
type Clock interface{ Now() time.Time }

// Step is what a caller does with a signal that needs a supervisor for a root.
type Step int

const (
	// Start: no start is on its way and none failed lately: this caller starts the supervisor (and calls End).
	Start Step = iota
	// Wait: a start for this root is on its way: the signal stays pending and goes to the supervisor once it exists.
	Wait
	// Fallback: the last start failed less than Cooldown ago: the signal goes to the main conversation as before.
	Fallback
)

// ClockFunc is a Clock that is a function.
type ClockFunc func() time.Time

// Now is f().
func (f ClockFunc) Now() time.Time { return f() }

// Gate keeps one start per root. It has no package state; its tests drive a fake clock.
type Gate struct {
	clock  Clock
	mu     sync.Mutex
	flying map[string]bool
	failed map[string]time.Time
}

// New is a Gate counting time by clock.
func New(clock Clock) *Gate {
	return &Gate{clock: clock, flying: map[string]bool{}, failed: map[string]time.Time{}}
}

// Begin says what to do about root and, for Start, records that a start is on its way: two callers asking for the same
// root get one Start.
func (g *Gate) Begin(root string) Step {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.flying[root] {
		return Wait
	}
	if at, ok := g.failed[root]; ok {
		if g.clock.Now().Sub(at) < Cooldown {
			return Fallback
		}
		delete(g.failed, root)
	}
	g.flying[root] = true
	return Start
}

// End ends the start Begin allowed; ok false starts the root's cooldown.
func (g *Gate) End(root string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.flying, root)
	if !ok {
		g.failed[root] = g.clock.Now()
	}
}

// Cooling is whether root waits out a cooldown now.
func (g *Gate) Cooling(root string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	at, ok := g.failed[root]
	return ok && g.clock.Now().Sub(at) < Cooldown
}

// Fail starts root's cooldown without a start of ours: its supervisor exists but cannot be reached.
func (g *Gate) Fail(root string) { g.End(root, false) }

// Above is the conversation above a supervisor Agent whose to-do has ledger supervisor and origin: the one that
// supervised the to-do before the Agent took it (above), else its ledger supervisor, else its origin, else main. An
// Agent's reference is no conversation (isAgent says so).
func Above(above, supervisor, origin, main string, isAgent func(string) bool) string {
	for _, c := range []string{above, supervisor, origin} {
		if c != "" && !isAgent(c) {
			return c
		}
	}
	return main
}

// Fit splits candidates (a supervisor's pieces to adopt, in order) into those that fit under most open workers, given
// that open of them are open already, and those that stay with the conversation above.
func Fit(open int, candidates []string, most int) (take, left []string) {
	room := max(0, most-open)
	if room >= len(candidates) {
		return candidates, nil
	}
	return candidates[:room], candidates[room:]
}
