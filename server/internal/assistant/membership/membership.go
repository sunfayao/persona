// Package membership holds the rules of several Agents working on one to-do: who is a working member, the assignment
// a member answers for and its validation, which member a message is for, what attach, detach and move may do, and
// which supervision signals are about one Agent's session rather than about the to-do. It takes plain values, keeps
// no state and imports neither the assistant root nor a sibling domain; the kind names are the wire names of the
// signals.
package membership

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SourceLeader is the todo_agents.source of a supervisor Agent. It supervises a to-do and is never one of its members.
const SourceLeader = "leader"

// The states of an assignment. A row with no assignment is a member that works, as every Agent did before members.
const (
	StateWorking = "working"
	StateDone    = "done"
)

const (
	// Most is how many working members one to-do may have (and, with its pieces, one supervisor Agent supervises).
	Most = 8
	// titleMax bounds a member's title, in characters.
	titleMax = 60
	// expectMax bounds a member's estimate: a day, as a to-do's is.
	expectMax = 24 * 60
	// ShortID is how many characters of a session id name it in a list.
	ShortID = 8
)

// Assignment is what one member answers for on its to-do: a title for its part, the texts of the to-do's criteria it
// is to show met, its own estimate, whether it reported its part done, and how many times it was sent back. The
// to-do keeps one ledger and one acceptance; the assignment only divides the work.
type Assignment struct {
	Title         string   `json:"title"`
	Criteria      []string `json:"criteria,omitempty"`
	ExpectMinutes int      `json:"expect_minutes,omitempty"`
	State         string   `json:"state,omitempty"`
	Round         int      `json:"round,omitempty"`
	// Extended counts the times the hub moved ExpectMinutes later on its own because the member made progress.
	Extended int `json:"extended,omitempty"`
}

// Decode reads a stored assignment; nil for none and for what cannot be read (the row then works on the whole to-do).
func Decode(raw []byte) *Assignment {
	if len(raw) == 0 {
		return nil
	}
	var a Assignment
	if json.Unmarshal(raw, &a) != nil {
		return nil
	}
	return &a
}

// Encode is the stored form of a; nil for nil.
func (a *Assignment) Encode() json.RawMessage {
	if a == nil {
		return nil
	}
	raw, _ := json.Marshal(a)
	return raw
}

// Clone is a copy that shares nothing with a.
func (a *Assignment) Clone() *Assignment {
	if a == nil {
		return nil
	}
	c := *a
	c.Criteria = slices.Clone(a.Criteria)
	return &c
}

// Member is one Agent row of a to-do, as the rules read it.
type Member struct {
	Machine, Session, Source string
	LinkedAt                 time.Time
	// Active: the row is neither released nor dismissed.
	Active     bool
	Assignment *Assignment
}

// Working says m is a working member: an unreleased row of an Agent that is not the to-do's supervisor Agent.
func (m Member) Working() bool { return m.Active && m.Session != "" && m.Source != SourceLeader }

// State is m's state in a list: its assignment's, else working.
func (m Member) State() string {
	if m.Assignment != nil && m.Assignment.State != "" {
		return m.Assignment.State
	}
	return StateWorking
}

// Title is m's title in a list: its assignment's, else the title of its row.
func (m Member) Title(row string) string {
	if m.Assignment != nil && m.Assignment.Title != "" {
		return m.Assignment.Title
	}
	return row
}

// Working are the working members of rows, in their order.
func Working(rows []Member) []Member {
	return slices.DeleteFunc(slices.Clone(rows), func(m Member) bool { return !m.Working() })
}

// Same says two rows are one Agent (machine ids compare as the caller canonicalised them).
func (m Member) Same(machine, session string) bool {
	return m.Machine == machine && m.Session == session
}

// ---- the assignment ----

// NewAssignment checks the part a new member is given and returns it. criteria name criteria of the to-do
// (todoCriteria, numbered from 1 as everywhere): each by its number ("2", "#2") or its exact text; none: the member
// shows no criterion of its own (a reader, a helper). A title is a short line; expect minutes are optional.
func NewAssignment(title string, criteria []string, expect int, todoCriteria []string) (Assignment, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Assignment{}, errors.New("a member needs a title: its part of the to-do in a short line")
	}
	if utf8.RuneCountInString(title) > titleMax {
		return Assignment{}, fmt.Errorf("the title of a member is at most %d characters", titleMax)
	}
	if expect < 0 || expect > expectMax {
		return Assignment{}, fmt.Errorf("expect_minutes is 0 (the to-do's) or at most %d", expectMax)
	}
	a := Assignment{Title: title, ExpectMinutes: expect, State: StateWorking}
	for _, c := range criteria {
		text, ok := resolve(strings.TrimSpace(c), todoCriteria)
		if !ok {
			return Assignment{}, fmt.Errorf("criterion %q is none of the to-do's: it has%s; a member's done_when names those, by number or text",
				c, Numbered(todoCriteria))
		}
		if !slices.Contains(a.Criteria, text) {
			a.Criteria = append(a.Criteria, text)
		}
	}
	return a, nil
}

func resolve(c string, todo []string) (string, bool) {
	if n, err := strconv.Atoi(strings.TrimPrefix(c, "#")); err == nil {
		if n >= 1 && n <= len(todo) {
			return todo[n-1], true
		}
		return "", false
	}
	if slices.Contains(todo, c) {
		return c, true
	}
	return "", false
}

// Numbered lists criteria as " 1. text 2. text", or " no criteria".
func Numbered(criteria []string) string {
	if len(criteria) == 0 {
		return " no criteria"
	}
	var b strings.Builder
	for i, c := range criteria {
		fmt.Fprintf(&b, " %d. %s", i+1, c)
	}
	return b.String()
}

// Numbers are the numbers (from 1) of a's criteria among the to-do's, in the to-do's order; a criterion the to-do no
// longer has is left out.
func (a Assignment) Numbers(todoCriteria []string) []int {
	var out []int
	for i, c := range todoCriteria {
		if slices.Contains(a.Criteria, c) {
			out = append(out, i+1)
		}
	}
	return out
}

// Expect is the minutes a member's clock runs: its assignment's estimate, else the to-do's.
func (m Member) Expect(todo int) int {
	if m.Assignment != nil && m.Assignment.ExpectMinutes > 0 {
		return m.Assignment.ExpectMinutes
	}
	return todo
}

// OwnClock says m has an estimate of its own: the to-do's clock is not its clock.
func (m Member) OwnClock() bool { return m.Assignment != nil && m.Assignment.ExpectMinutes > 0 }

// DueAt is when m's own estimate is due: from the moment it was linked. Zero for a member with no estimate of its own
// or one that reported its part done.
func (m Member) DueAt() time.Time {
	if !m.OwnClock() || m.State() == StateDone {
		return time.Time{}
	}
	return m.LinkedAt.Add(time.Duration(m.Assignment.ExpectMinutes) * time.Minute)
}

// ---- which member a message is for ----

// Pick is the member a message about a to-do is for: the one named (by session id or a prefix of it), the only
// working one when none is named, else a refusal that lists the working members and says nothing was done. found false
// with an empty refusal: the to-do has no working member.
func Pick(rows []Member, named string, rowTitle func(Member) string) (m Member, found bool, refusal string) {
	members := Working(rows)
	named = strings.TrimSpace(named)
	switch {
	case len(members) == 0:
		return Member{}, false, ""
	case named != "":
		var hit []Member
		for _, c := range members {
			if c.Session == named || len(named) >= 4 && strings.HasPrefix(c.Session, named) {
				hit = append(hit, c)
			}
		}
		if len(hit) == 1 {
			return hit[0], true, ""
		}
		return Member{}, false, fmt.Sprintf("session_id %q names %s of the to-do's members; %s", named, countWord(len(hit)), List(members, rowTitle))
	case len(members) == 1:
		return members[0], true, ""
	}
	return Member{}, false, "this to-do has several working members: name the one it is for (session_id), nothing was done; " + List(members, rowTitle)
}

func countWord(n int) string {
	if n == 0 {
		return "none"
	}
	return "more than one"
}

// List is the members as one message: "members: <session> (title, state); ...".
func List(members []Member, rowTitle func(Member) string) string {
	parts := make([]string, 0, len(members))
	for _, m := range members {
		title := m.Title("")
		if title == "" && rowTitle != nil {
			title = rowTitle(m)
		}
		parts = append(parts, fmt.Sprintf("%s (%s, %s)", Short(m.Session), title, m.State()))
	}
	return "members: " + strings.Join(parts, "; ")
}

// Short is the first characters of a session id.
func Short(id string) string {
	if len(id) > ShortID {
		return id[:ShortID]
	}
	return id
}

// Others counts the working members other than machine/session whose part is not reported done and that alive says
// are still going: the Agents a done report does not yet complete the to-do over.
func Others(rows []Member, machine, session string, alive func(Member) bool) int {
	n := 0
	for _, m := range Working(rows) {
		if !m.Same(machine, session) && m.State() != StateDone && (alive == nil || alive(m)) {
			n++
		}
	}
	return n
}

// ---- attach, detach, move ----

// Action is what agent_supervise does to an Agent and a to-do.
type Action string

const (
	Attach Action = "attach"
	Detach Action = "detach"
	Move   Action = "move"
)

// ParseAction reads agent_supervise's action; "" is no action (the tool's older meaning).
func ParseAction(s string) (Action, error) {
	switch a := Action(strings.TrimSpace(s)); a {
	case "", Attach, Detach, Move:
		return a, nil
	}
	return "", fmt.Errorf("action %q is none of attach, detach, move", s)
}

// View is a to-do as attach, detach and move see it.
type View struct {
	ID string
	// Open: not closed. Supervised: the caller supervises it. InProgress: open and neither waiting for review nor
	// with the owner.
	Open, Supervised, InProgress bool
	Members                      []Member
}

// Check says whether action may be done for the Agent machine/session: on target for attach and detach, from from to
// target for move (from is the to-do the Agent is a member of now). It returns the one sentence that refuses it.
func Check(action Action, machine, session string, target View, from *View) error {
	if !target.Open {
		return fmt.Errorf("to-do %s is closed; nothing was changed", target.ID)
	}
	if !target.Supervised {
		return fmt.Errorf("to-do %s is not supervised from here; nothing was changed", target.ID)
	}
	member := func(v View) (Member, bool) {
		for _, m := range Working(v.Members) {
			if m.Same(machine, session) {
				return m, true
			}
		}
		return Member{}, false
	}
	leaves := func(v View) error {
		if _, ok := member(v); !ok {
			return fmt.Errorf("that Agent is no working member of to-do %s; nothing was changed", v.ID)
		}
		if v.InProgress && len(Working(v.Members)) == 1 {
			return fmt.Errorf("it is the last Agent working on to-do %s, which is in progress: attach another Agent first, "+
				"or end the to-do with task_review; nothing was changed", v.ID)
		}
		return nil
	}
	switch action {
	case Attach:
		if _, ok := member(target); ok {
			return fmt.Errorf("that Agent is a member of to-do %s already; nothing was changed", target.ID)
		}
		if len(Working(target.Members)) >= Most {
			return fmt.Errorf("to-do %s has %d working members already, the most; nothing was changed", target.ID, Most)
		}
	case Detach:
		return leaves(target)
	case Move:
		if from == nil || from.ID == target.ID {
			return errors.New("move takes the Agent from the to-do it is on to another: todo_id names the other; nothing was changed")
		}
		if !from.Supervised {
			return fmt.Errorf("to-do %s is not supervised from here; nothing was changed", from.ID)
		}
		if err := leaves(*from); err != nil {
			return err
		}
		return Check(Attach, machine, session, target, nil)
	}
	return nil
}

// Load is how many members one supervisor Agent supervises at a time (the bound is Most): each piece counts its working
// members, at least one while it is open (its Agent may not be linked yet); the request's own to-do counts only its
// members, when it is member-supervised.
func Load(pieces []int, own int) int {
	n := own
	for _, p := range pieces {
		n += max(p, 1)
	}
	return n
}
