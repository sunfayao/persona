package ownerattention

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"slices"
)

// ---- children folded into their parent (#5120), a tab's workers into its lead (#5180), side sessions into the
// session they were started from (#7944) ----
// docs/protocol/protocol.md "Session hierarchy"

// Fold is what folding says of one machine's list: the session each folded row folds under (its topmost
// listed ancestor), why for a row that is no child (as: proto.FoldedAsWorker or proto.FoldedAsSide), the side session
// a row folded through one belongs to (side, not set on that side itself), and the rollup each such ancestor carries
// of all the rows folded under it.
type Fold struct {
	Under    map[string]string
	As       map[string]string
	Side     map[string]string
	Children map[string]*proto.SessionChildren
}

// tabKey is a host tab of one machine's list (a herdr tab): its space kind, space and slot.
type tabKey struct{ kind, space, slot string }

// FoldSessions folds a machine's complete list (its rows as decorated, their states computed). Only the rows the apps
// are shown are listed: not an archived Agent (the hub leaves those out, deleted_sessions.go), nor one in hidden (a
// review's reviewer, which publishList leaves out). A child (a subagent, or a session that names a parent or another
// root, proto.Session.IsChild) has as its parent the listed row its parent_session_id names, else the one its
// asking_parent_session_id names, else its root_session_id's; it folds under the topmost listed ancestor that parent
// leads to. A side session (sideParents, #7944) has as its parent the row its parent_agent_id names and is marked
// proto.FoldedAsSide; a row that folds through a side further up names the nearest one (folded_side). A tab's worker
// (tabLeads, #5180) has its tab's lead as its parent and is marked proto.FoldedAsWorker; a side is in no tab team. An
// orphan (no listed parent) and a row whose ancestors go round in a cycle fold under nothing. Each ancestor that rows
// fold under counts them all, whatever their depth, by their own states, how many of them are its workers, and how
// many are side sessions, by their own states.
func FoldSessions(list []proto.Session, hidden map[string]bool) Fold {
	listed := func(s *proto.Session) bool {
		return (!s.Archived || s.Assistant) && !s.HiddenByDirectory() && !hidden[s.SessionID]
	}
	if !mayFold(list, listed) {
		return Fold{} // what the walk below finds for such a list, without the index it builds
	}
	at := make(map[string]int, len(list))
	for i := range list {
		if _, dup := at[list[i].SessionID]; !dup && listed(&list[i]) {
			at[list[i].SessionID] = i
		}
	}
	counted := func(i int) bool { j, ok := at[list[i].SessionID]; return ok && j == i }
	sides := sideParents(list, at, counted)
	lead := tabLeads(list, func(i int) bool { _, side := sides[i]; return counted(i) && !side })
	parent := func(i int) int {
		s := &list[i]
		if !s.IsChild() {
			if j, ok := sides[i]; ok {
				return j
			}
			if j, ok := lead[i]; ok {
				return j
			}
			return -1
		}
		root := s.RootSessionID
		if root == s.SessionID {
			root = ""
		}
		for _, id := range [...]string{s.ParentSessionID, s.AskingParentSessionID(), root} {
			if j, ok := at[id]; ok && id != "" && j != i {
				return j
			}
		}
		return -1
	}
	var f Fold
	var path []int // the rows walked up from one child, to find a cycle
	for i := range list {
		if at[list[i].SessionID] != i || !listed(&list[i]) {
			continue
		}
		top := i
		path = append(path[:0], i)
		for {
			p := parent(top)
			if p < 0 {
				break
			}
			if slices.Contains(path, p) {
				top = i // a cycle: no topmost ancestor
				break
			}
			path = append(path, p)
			top = p
		}
		if top == i {
			continue
		}
		if f.Under == nil {
			f.Under, f.Children = map[string]string{}, map[string]*proto.SessionChildren{}
		}
		id, root := list[i].SessionID, list[top].SessionID
		f.Under[id] = root
		c := f.Children[root]
		if c == nil {
			c = &proto.SessionChildren{}
			f.Children[root] = c
		}
		c.Total++
		switch list[i].State {
		case proto.StateWorking:
			c.Working++
		case proto.StateNeedsYou:
			c.NeedsYou++
		}
		_, side := sides[i]
		if _, worker := lead[i]; worker || side { // a worker or a side itself, not a row folded through one
			if f.As == nil {
				f.As = map[string]string{}
			}
			if side {
				f.As[id] = proto.FoldedAsSide
				c.Sides++
				switch list[i].State {
				case proto.StateWorking:
					c.SidesWorking++
				case proto.StateNeedsYou:
					c.SidesNeedsYou++
				case proto.StateUnread:
					c.SidesUnread++
				}
			} else {
				f.As[id] = proto.FoldedAsWorker
				c.Workers++
			}
			continue
		}
		// The nearest side on the way up, the topmost row aside (it folds under nothing, so it is no side here).
		for _, p := range path[1 : len(path)-1] {
			if _, ok := sides[p]; ok {
				if f.Side == nil {
					f.Side = map[string]string{}
				}
				f.Side[id] = list[p].SessionID
				break
			}
		}
	}
	return f
}

// mayFold: a row of list may fold, so FoldSessions has to walk it: a listed row is a child, a listed row may be a side
// (sideSeat), or listed rows that can be of a tab's team (tabSeat) sit in a first pane (a lead) and in another pane (a
// worker; a second row in its tab's first pane would leave it without a lead). None of these, and no row has a parent,
// so nothing folds. Each test asks less than FoldSessions does (a side's parent_agent_id need name no listed row here),
// and rows listed twice count each time: that can only say a fold is possible where FoldSessions finds none.
func mayFold(list []proto.Session, listed func(*proto.Session) bool) bool {
	var first, other bool
	for i := range list {
		s := &list[i]
		switch {
		case !listed(s):
		case s.IsChild(), sideSeat(s):
			return true
		case tabSeat(s):
			if p := s.PaneIndex; p != nil && *p == 0 {
				first = true
			} else {
				other = true
			}
			if first && other {
				return true
			}
		}
	}
	return false
}

// sideSeat: s can be a side session as far as the row alone tells (sideParents): no child and not the assistant's,
// naming a parent_agent_id other than itself.
func sideSeat(s *proto.Session) bool {
	id := s.ParentAgentID()
	return id != "" && id != s.SessionID && !s.Assistant && !s.IsChild()
}

// tabSeat: s can be of a tab's team as far as the row alone tells (tabLeads): a session, not the assistant's, in a
// named space and slot, no child and not offline.
func tabSeat(s *proto.Session) bool {
	return !s.Assistant && s.SessionID != "" && s.SpaceID != "" && s.SlotID != "" && !s.IsChild() && s.State != proto.StateOffline
}

// sideParents finds the side sessions of a machine's list (#7944): for each, by its index, the index of the row its
// parent_agent_id names. A side is a counted row (the listed rows, each id once) that is no child and no assistant row
// and names another counted row that is no assistant row either; that row need not run. A child keeps folding by its
// own parent fields.
func sideParents(list []proto.Session, at map[string]int, counted func(int) bool) map[int]int {
	var out map[int]int
	for i := range list {
		s := &list[i]
		id := s.ParentAgentID()
		if id == "" || id == s.SessionID || s.Assistant || s.IsChild() || !counted(i) {
			continue
		}
		if j, ok := at[id]; ok && j != i && !list[j].Assistant {
			if out == nil {
				out = map[int]int{}
			}
			out[i] = j
		}
	}
	return out
}

// tabLeads finds the tab teams of a machine's list (#5180): for each worker, by its index, its lead's index. A tab is
// the rows of one space kind, space and slot, both named; a row of the assistant's is in none. Its lead is the one row
// counted (the listed rows, each id once) in its first pane (pane_index 0) that is no child and not offline; a tab with
// none or more than one such row has no lead, its Agents peers. Every other counted row of a tab with a lead that is
// no child (a child folds by its own parent) and not offline (a stopped pane has left the team) is its worker.
func tabLeads(list []proto.Session, counted func(int) bool) map[int]int {
	member := func(i int) (tabKey, bool) {
		s := &list[i]
		if !tabSeat(s) || !counted(i) {
			return tabKey{}, false
		}
		return tabKey{s.SpaceKind, s.SpaceID, s.SlotID}, true
	}
	var leads map[tabKey]int // the lead's index; -1 for a tab with more than one row in its first pane
	for i := range list {
		if p := list[i].PaneIndex; p == nil || *p != 0 {
			continue
		}
		if k, ok := member(i); ok {
			if leads == nil {
				leads = map[tabKey]int{}
			}
			if _, two := leads[k]; two {
				leads[k] = -1
			} else {
				leads[k] = i
			}
		}
	}
	var out map[int]int
	for i := range list {
		k, ok := member(i)
		if !ok {
			continue
		}
		if j, has := leads[k]; has && j >= 0 && j != i {
			if out == nil {
				out = map[int]int{}
			}
			out[i] = j
		}
	}
	return out
}

// apply puts the fold on the rows: what each folds under, why, and the rollup it carries, nothing on the others. A row
// is given a new hierarchy only where it changes.
func (f Fold) Apply(list []proto.Session) {
	for i := range list {
		id := list[i].SessionID
		list[i].SetFold(f.Under[id], f.As[id], f.Side[id], f.Children[id])
	}
}
