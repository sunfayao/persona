// Package parentcloseout holds the rules for closing the to-do that stands for a request in pieces: which parent is a
// plain container of dispatched pieces, when its pieces count as finished, how long the set must stay still, when a
// wake that nobody answered is raised again, and the roll-up the hub writes when it closes a container itself. It reads
// plain values only; the assistant root turns its to-dos into them and acts on the verdict.
package parentcloseout

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

const (
	// SettleWindow is how long the parent's set of pieces, and their states, must have stayed unchanged before anything
	// is closed or raised: pieces are attached one call at a time.
	SettleWindow = 90 * time.Second
	// ReraiseAfter is how long a delivered wake waits for an answer before it is raised again; MaxWakes caps the wakes.
	ReraiseAfter = 30 * time.Minute
	MaxWakes     = 3

	// The states a finished parent shows the owner (the view's steps.closeout).
	StateClosing  = "closing"
	StateAwaiting = "awaiting"

	// SettingKey is where the wake markers are kept.
	SettingKey = "assistant.parent_closeout_wakes"
)

// Piece is one to-do whose part_of names the parent.
type Piece struct {
	ID, Title    string
	Done         bool // closed, whatever way
	Cancelled    bool
	CancelReason string
	StartFailed  bool // its Agent did not start
	Dispatched   bool // has an origin conversation or a supervision ledger
	Changed      time.Time
	Criteria     int // acceptance criteria it had, and how many were met
	Met          int
	PRs          []string // delivery or merged PR links
}

// Parent is the to-do the pieces belong to.
type Parent struct {
	Open      bool // not done, not cancelled, no start error
	Excluded  bool // a concern, an ask or an outside decision
	HasAgent  bool // an Agent works on it now
	OwnAgent  bool // an Agent was ever linked to it
	HasLedger bool // acceptance criteria of its own (a supervision ledger)
	Parked    bool
	HoldUntil time.Time // a reminder or due moment still ahead holds the parent open
	Reopened  time.Time
	Updated   time.Time // its latest write, a note or a park included
}

// Marker records that a wake was delivered for the pieces named by Key.
type Marker struct {
	Key    string    `json:"key"`
	Target string    `json:"target,omitempty"`
	At     time.Time `json:"at"`
	Count  int       `json:"count,omitempty"` // wakes delivered for Key; absent on markers written before re-raising
}

// Action is what the hub does with a parent now.
type Action int

const (
	ActionNone  Action = iota
	ActionWait         // finished, but the settle window is not over
	ActionClose        // a container: close it with the roll-up
	ActionWake         // raise the closeout wake
)

// Verdict is the outcome of Evaluate. Key is the wake's key, with a "#n" attempt suffix after the first.
type Verdict struct {
	Action    Action
	State     string
	Container bool
	Key       string
}

// Clock is the time the rules read.
type Clock interface{ Now() time.Time }

// ClockFunc is a Clock that is a function.
type ClockFunc func() time.Time

// Now is f().
func (f ClockFunc) Now() time.Time { return f() }

// Store is the settings table the markers live in.
type Store interface {
	Setting(ctx context.Context, key string, v any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

// Dependencies of New; Clock defaults to the wall clock.
type Dependencies struct {
	Clock Clock
	Store func() Store
	Logf  func(string, ...any)
}

// Domain holds the rules' clock and the wake markers.
type Domain struct {
	clock Clock
	store func() Store
	logf  func(string, ...any)

	mu     sync.Mutex
	loaded bool
	marks  map[string]Marker
}

// New makes a Domain.
func New(deps Dependencies) *Domain {
	d := &Domain{clock: deps.Clock, store: deps.Store, logf: deps.Logf}
	if d.clock == nil {
		d.clock = ClockFunc(time.Now)
	}
	if d.store == nil {
		d.store = func() Store { return nil }
	}
	if d.logf == nil {
		d.logf = func(string, ...any) {}
	}
	return d
}

// IsContainer: a parent with no criteria of its own, no Agent of its own, that is no concern, ask or outside decision,
// and whose pieces are all dispatched tasks. It is read off the data, so parents made before it existed qualify.
func IsContainer(p Parent, pieces []Piece) bool {
	return !p.HasLedger && !p.HasAgent && !p.OwnAgent && !p.Excluded && len(pieces) > 0 &&
		!slices.ContainsFunc(pieces, func(c Piece) bool { return !c.Dispatched })
}

// Accepted: the piece is done and was neither cancelled nor failed to start.
func (c Piece) Accepted() bool { return c.Done && !c.Cancelled && !c.StartFailed }

// Ready: no piece is open, at least one was accepted, and no failed start is left without a replacement (an accepted
// piece with the same title).
func Ready(pieces []Piece) bool {
	accepted := map[string]bool{}
	for _, c := range pieces {
		if c.Accepted() {
			accepted[strings.ToLower(strings.TrimSpace(c.Title))] = true
		}
	}
	for _, c := range pieces {
		switch {
		case c.Cancelled || c.Accepted():
		case c.StartFailed && accepted[strings.ToLower(strings.TrimSpace(c.Title))]:
		default:
			return false
		}
	}
	return len(accepted) > 0
}

// Key identifies the finished set of pieces (and the parent's reopen generation) a wake was raised for.
func Key(pieces []Piece, reopened time.Time) string {
	ids := make([]string, 0, len(pieces))
	for _, c := range pieces {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	key := "children:" + strings.Join(ids, ",")
	if !reopened.IsZero() {
		key += "@reopen:" + reopened.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	}
	return key
}

// SameSet says whether wake key a belongs to the finished set that key b names, whatever attempt each is.
func SameSet(a, b string) bool {
	a, _, _ = strings.Cut(a, "#")
	b, _, _ = strings.Cut(b, "#")
	return a == b
}

// Evaluate decides what to do with parent p whose pieces are given; m is the wake marker kept for it, if any.
func (d *Domain) Evaluate(p Parent, pieces []Piece, m *Marker) Verdict {
	if !p.Open || p.Excluded || p.HasAgent || !Ready(pieces) {
		return Verdict{}
	}
	now := d.clock.Now()
	var last time.Time
	for _, c := range pieces {
		if c.Changed.After(last) {
			last = c.Changed
		}
	}
	v := Verdict{Key: Key(pieces, p.Reopened), Container: IsContainer(p, pieces), State: StateAwaiting}
	settled := now.Sub(last) >= SettleWindow
	if v.Container {
		if p.HoldUntil.After(now) || p.Reopened.After(last) { // held by a date, or reopened by the owner after the pieces ended
			return Verdict{Container: true}
		}
		v.State = StateClosing
	}
	switch {
	case !settled:
		v.Action = ActionWait
	case v.Container:
		v.Action = ActionClose
	case m == nil || !SameSet(m.Key, v.Key):
		v.Action = ActionWake
	case m.attempts() < MaxWakes && now.Sub(m.At) >= ReraiseAfter && !p.Parked && !p.Updated.After(m.At):
		v.Action, v.Key = ActionWake, v.Key+"#"+strconv.Itoa(m.attempts()+1)
	}
	return v
}

func (m Marker) attempts() int { return max(m.Count, 1) }

// RollUp is the result text of a closed container: each piece's verdict (with the cancel reason, the criteria met and
// the delivery links) in the owner's language, one line each under a heading.
func RollUp(l i18n.Lang, pieces []Piece) string {
	accepted := 0
	for _, c := range pieces {
		if c.Accepted() {
			accepted++
		}
	}
	lines := []string{i18n.T(l, "assistant-todos.closeout_head", i18n.Args{"accepted": accepted, "cancelled": len(pieces) - accepted})}
	for _, c := range pieces {
		a := i18n.Args{"title": c.Title}
		switch {
		case c.Accepted():
			line := i18n.T(l, "assistant-todos.closeout_accepted", a)
			if c.Criteria > 0 {
				a["met"], a["total"] = c.Met, c.Criteria
				line = i18n.T(l, "assistant-todos.closeout_accepted_criteria", a)
			}
			if len(c.PRs) > 0 {
				line += i18n.T(l, "assistant-todos.closeout_prs", i18n.Args{"links": strings.Join(c.PRs, ", ")})
			}
			lines = append(lines, line)
		case c.Cancelled && strings.TrimSpace(c.CancelReason) != "":
			a["reason"] = strings.Join(strings.Fields(c.CancelReason), " ")
			lines = append(lines, i18n.T(l, "assistant-todos.closeout_cancelled_reason", a))
		case c.Cancelled:
			lines = append(lines, i18n.T(l, "assistant-todos.closeout_cancelled", a))
		}
	}
	return strings.Join(lines, "\n")
}

// ---- wake markers ----

// load reads the markers once per Domain; d.mu is held.
func (d *Domain) load(ctx context.Context) error {
	if d.loaded {
		return nil
	}
	loaded := map[string]Marker{}
	if st := d.store(); st != nil {
		if _, err := st.Setting(ctx, SettingKey, &loaded); err != nil {
			return err
		}
	} else if d.marks != nil {
		loaded = d.marks
	}
	d.marks, d.loaded = loaded, true
	return nil
}

// save keeps next as the markers: in the store when there is one, then in memory.
func (d *Domain) save(ctx context.Context, next map[string]Marker, what string) {
	if st := d.store(); st != nil {
		if err := st.PutSetting(ctx, SettingKey, next); err != nil {
			d.logf("parent_closeout: %s: %v", what, err)
			return
		}
	}
	d.marks = next
}

// Marker is the wake marker kept for parent id.
func (d *Domain) Marker(ctx context.Context, id string) (Marker, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(ctx); err != nil {
		return Marker{}, false, err
	}
	m, ok := d.marks[id]
	return m, ok, nil
}

// Told records that the wake of key was delivered or durably queued for parent id, to target.
func (d *Domain) Told(ctx context.Context, id, key, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(ctx); err != nil {
		d.logf("parent_closeout: load wake markers: %v", err)
		return
	}
	cur, ok := d.marks[id]
	if ok && cur.Key == key {
		return
	}
	n := 1
	if ok && SameSet(cur.Key, key) {
		n = cur.attempts() + 1
	}
	next := maps.Clone(d.marks)
	next[id] = Marker{Key: key, Target: target, At: d.clock.Now().UTC(), Count: n}
	d.save(ctx, next, "persist wake markers")
}

// Forget drops the markers of the parents for which gone says so (closed or deleted), or of id alone when gone is nil.
func (d *Domain) Forget(ctx context.Context, id string, gone func(id string) bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(ctx); err != nil {
		d.logf("parent_closeout: load wake markers for reset: %v", err)
		return
	}
	next := maps.Clone(d.marks)
	for k := range d.marks {
		if gone == nil && k == id || gone != nil && gone(k) {
			delete(next, k)
		}
	}
	if len(next) != len(d.marks) {
		d.save(ctx, next, "clear wake markers")
	}
}
