package assistant

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// liveState wakes liveLoop.
type liveState struct {
	mu     sync.Mutex
	signal chan struct{}
}

func (l *liveState) ch() chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.signal == nil {
		l.signal = make(chan struct{}, 1)
	}
	return l.signal
}

// liveChanged: what the fleet is made of may have changed.
func (s *Service) liveChanged() {
	if !s.fleetOn() {
		return
	}
	select {
	case s.fleet.live.ch() <- struct{}{}:
	default:
	}
}

// liveLoop works out Persona Link's summary of the fleet when it changed, at most every 100 ms, and looks again every
// 15 s; the devices get it.
func (s *Service) liveLoop(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	sig := s.fleet.live.ch()
	// Each machine's list as the last round decorated it. While the fleet keeps changing, the next round copies the
	// rows over them instead of into new arrays: a copy of every machine's list up to ten times a second was half of
	// what the manager allocated under churn (#3197). The 15 s look takes new arrays and gives these back, so a
	// fleet gone quiet holds no second copy of its lists.
	var lists map[string][]proto.Session
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			time.Sleep(100 * time.Millisecond)
			if lists == nil {
				lists = map[string][]proto.Session{}
			}
		case <-tick.C:
			lists = nil
		}
		l := s.lang(context.Background())
		agents, ixs, told := s.fleetInputsInto(lists)
		s.devs.SetAttention(linkAttention(l, agents, ixs, told)) // sent on only when it changed
	}
}

// fleetInputs is every machine's agents as the fleet sees them, the pending interactions (oldest first), and which of
// those are the user's (pendingCards). Each agent points into its machine's decorated list, a copy of its own that
// nothing else holds and the fleet only reads: liveLoop builds the fleet up to ten times a second, and a copy of every
// session per agent (1.5 KB each) was most of what that allocated.
func (s *Service) fleetInputs() ([]fleetInput, []proto.Interaction, map[string]bool) {
	return s.fleetInputsInto(nil)
}

// fleetInputsInto is fleetInputs with each machine's rows copied over the list kept for it in kept (liveLoop's own,
// from its last round), which then holds this round's lists; nil takes new arrays, as fleetInputs. Every round still
// copies and decorates every list whole, only into the arrays of the last one. The agents point into kept's lists
// until the next round: the summary it makes keeps strings and numbers, never a
// row or a pointer to one.
func (s *Service) fleetInputsInto(kept map[string][]proto.Session) ([]fleetInput, []proto.Interaction, map[string]bool) {
	machines := s.fleetMachines()
	lists := make([][]proto.Session, len(machines))
	n := 0
	for i, m := range machines {
		// A machine listed twice would decorate into the same array twice: only its first list is kept.
		if kept == nil || slices.ContainsFunc(machines[:i], func(o fleetMachine) bool { return o.ID == m.ID }) {
			lists[i] = s.decorated(m.ID)
		} else {
			lists[i] = s.decoratedInto(m.ID, kept[m.ID])
			keepList(kept, m.ID, lists[i])
		}
		n += len(lists[i])
	}
	if len(kept) > 0 {
		maps.DeleteFunc(kept, func(id string, _ []proto.Session) bool { // the machines that went
			return !slices.ContainsFunc(machines, func(m fleetMachine) bool { return m.ID == id })
		})
	}
	agents := make([]fleetInput, 0, n)
	for i, m := range machines {
		list := lists[i]
		for j := range list {
			sess := &list[j]
			if sess.Quiet || sess.HiddenByDirectory() { // a quiet task's agent: the main conversation tells about it (#1731)
				continue
			}
			agents = append(agents, fleetInput{
				// Its own space, not muted's lookup of the node's row: that scanned the machine's list once per session.
				Machine: m.Name, Session: sess, Muted: s.mutedAs(m.ID, sess.SessionID, sess.SpaceName),
			})
		}
	}
	ixs, told := s.visiblePendingCards()
	return agents, ixs, told
}

// keepList keeps list as the machine's for the next round. A list shorter than the last one is in the same array
// (it did not outgrow it): the rows it left behind are cleared, so they hold nothing of the node's older lists.
func keepList(kept map[string][]proto.Session, machine string, list []proto.Session) {
	if old := kept[machine]; len(list) < len(old) {
		clear(old[len(list):])
	}
	kept[machine] = list
}

// fleetInput is one agent as the fleet sees it. Session is read, never written: it points into a list the fleet's
// caller owns.
type fleetInput struct {
	Machine string // display name
	Session *proto.Session
	Muted   bool
}

// needer is an agent that needs the user, and the card that stands for it: its oldest (nil: it waits in a terminal
// dialog without one). card points into the cards needsYou was given.
type needer struct {
	agent fleetInput
	card  *proto.Interaction
}

// agentKey is an agent's machine and session, what its cards name it by.
type agentKey = [2]string

func keyOf(sess *proto.Session) agentKey { return agentKey{sess.MachineID, sess.SessionID} }

// needsYou is the agents that need the user, one each, as the Agents list has them (#3571): those with a card by their
// oldest card that stands (ixs come oldest first), then those in a terminal dialog without one, in agents' order.
// counts says which agents count at all, stands which cards may stand for one; the assistant's own dialog never
// counts. The caller sets which agents count and which cards represent them.
func needsYou(agents []fleetInput, ixs []proto.Interaction, counts func(fleetInput) bool, stands func(proto.Interaction) bool) []needer {
	var out []needer
	var asked map[agentKey]bool
	if len(ixs) > 0 {
		byKey := make(map[agentKey]int, len(agents)) // the agent's place in agents: the last one listed under its key
		for i, a := range agents {
			if counts(a) {
				byKey[keyOf(a.Session)] = i
			}
		}
		asked = map[agentKey]bool{}
		for i := range ixs {
			ix := &ixs[i]
			key := agentKey{ix.MachineID, ix.SessionID}
			if a, ok := byKey[key]; ok && !asked[key] && stands(*ix) {
				asked[key] = true
				out = append(out, needer{agent: agents[a], card: ix})
			}
		}
	}
	for _, a := range agents {
		if counts(a) && a.Session.State == proto.StateNeedsYou && !a.Session.Assistant && !asked[keyOf(a.Session)] {
			out = append(out, needer{agent: a})
		}
	}
	return out
}

// cardStands: the card may stand for its agent in Persona Link’s summary. An elicitation
// does not: the Fleet is shared with legacy clients, whose terminal fallback stays (the agent shows as in a dialog).
func cardStands(ix proto.Interaction) bool { return ix.Kind != proto.IxElicitation }

func agentTitle(sess *proto.Session, n int) string {
	t := transcript.Title(sess.Title)
	if t == "" || t == "Claude Code" {
		t = path.Base(sess.Cwd)
	}
	return clip(oneLine(t), n)
}

// agentPlace is "machine · space › tab", the subtitle pushes use too, in at most n runes.
func agentPlace(machine string, sess *proto.Session, n int) string {
	place := machine
	if sess.SpaceName != "" {
		place += " · " + sess.SpaceName
		if sess.SlotName != "" && sess.SlotName != fmt.Sprint(sess.SlotIndex) {
			place += " › " + sess.SlotName
		}
	}
	return clip(oneLine(place), n)
}

func interactionDetail(l i18n.Lang, ix proto.Interaction) string {
	switch ix.Kind {
	case proto.IxPermission:
		if ix.Summary != "" {
			return ix.Summary
		}
		return ixToolName(ix)
	case proto.IxPlan:
		return planTitle(ix.Plan)
	case proto.IxQuestion:
		if len(ix.Questions) > 0 {
			return ix.Questions[0].Question
		}
		return ix.Summary
	}
	return i18n.T(l, "assistant-link_attention.screen")
}
