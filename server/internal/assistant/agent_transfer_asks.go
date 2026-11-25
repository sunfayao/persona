package assistant

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// currentSupervisorSignal uses the same persisted ordinary recipient at admission, dispatch and queue recheck.
// Ask rungs and owner escalations retain their distinct state. It also repairs a supervisor-write/queue-checkpoint crash.
func (s *Service) currentSupervisorSignal(g superviseSignal) superviseSignal {
	if g.Kind != sigAsk {
		if t, ok := s.todoNow(g.Todo); ok && t.Supervision.assigned() && g.Kind != sigEscalated {
			if g.To != t.Supervision.Supervisor {
				g.batchTo, g.batchUntil = "", time.Time{}
			}
			g.To = t.Supervision.Supervisor
		}
		return g
	}
	id, step, _ := strings.Cut(g.Key, "@")
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if p := s.fleet.interactions[id]; p != nil && !p.claimed && !p.told && p.route != nil &&
		p.route.todo == g.Todo && p.route.step == 0 && !p.route.delivered {
		g.To = p.route.at()
		g.batchTo, g.batchUntil = "", time.Time{}
	} else if s.fleet.interactions[id] == nil && step == "0" {
		if t, ok := s.todoNow(g.Todo); ok && t.Supervision != nil && t.Supervision.assigned() {
			g.To = t.Supervision.Supervisor
		}
	}
	return g
}

func (s *Service) askRelayDelivered(to string, msg relayMsg) {
	signals, _ := decodeDigestSignals(msg.Signals)
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	for _, g := range signals {
		if g.Kind != sigAsk {
			continue
		}
		for _, p := range s.fleet.interactions {
			if r := p.route; r != nil && r.todo == g.Todo && r.at() == to && (len(g.Cards) == 0 || slices.ContainsFunc(g.Cards, func(card prompts.SuperviseAsk) bool { return card.ID == p.ix.ID })) {
				r.delivered = true
			}
		}
	}
}

// A restored card already represented by a durable queued ask must not create a second notification while that
// queue is being retargeted. The structured ask is rendered from the current held cards at the last boundary.
func (s *Service) askAlreadyQueued(todo, to string) bool {
	r := &s.relays
	r.mu.Lock()
	defer r.mu.Unlock()
	if !s.relayLocked(context.Background()) {
		return false
	}
	for _, queue := range r.st.Queue {
		for _, msg := range queue {
			if msg.Tool != superviseTool || !msg.TriedAt.IsZero() || msg.Withdrawn {
				continue
			}
			list, _ := decodeDigestSignals(msg.Signals)
			for _, g := range list {
				if g.Todo == todo && g.Kind == sigAsk && s.currentSupervisorSignal(g).To == to {
					return true
				}
			}
		}
	}
	return false
}
