package assistant

import (
	"github.com/sunfayao/persona/server/internal/assistant/ownerattention"

	"github.com/sunfayao/persona/server/internal/proto"
)

type sessionFold = ownerattention.Fold

func foldSessions(list []proto.Session, hidden map[string]bool) sessionFold {
	return ownerattention.FoldSessions(list, hidden)
}

// foldInventory folds machine's complete list (foldSessions, without the reviewers publishList hides), puts the fold on
// its rows and keeps it for the rows decorated on their own (foldSubset) and the cards sent to the apps (withTask)
// until the next complete list.
func (s *Service) foldInventory(machine string, list []proto.Session) {
	// Include remembered archive rows added after live inventory decoration.
	for i := range list {
		list[i].SetHiddenByDirectory(s.hiddenByDirectory(machine, list[i]))
	}
	var hidden map[string]bool
	for k := range s.reviewerSessions() {
		if k[0] == machine {
			if hidden == nil {
				hidden = map[string]bool{}
			}
			hidden[k[1]] = true
		}
	}
	f := foldSessions(list, hidden)
	f.Apply(list)
	s.fleet.foldMu.Lock()
	defer s.fleet.foldMu.Unlock()
	if s.fleet.folds == nil {
		s.fleet.folds = map[string]sessionFold{}
	}
	s.fleet.folds[machine] = f
}

// foldSubset puts on rows decorated without the rest of their machine's list what its last complete list folded them
// as: a single row pushed on its own keeps its folded_under, folded_as, folded_side and children.
func (s *Service) foldSubset(machine string, list []proto.Session) {
	s.fleet.foldMu.Lock()
	f := s.fleet.folds[machine]
	s.fleet.foldMu.Unlock()
	f.Apply(list)
}

// foldedUnder is the session machine's session sid folded under in its last complete list, why when it is no child
// (proto.FoldedAsWorker, proto.FoldedAsSide) and the side session it belongs to when it folds through one; "" for
// none.
func (s *Service) foldedUnder(machine, sid string) (under, as, side string) {
	s.fleet.foldMu.Lock()
	defer s.fleet.foldMu.Unlock()
	f := s.fleet.folds[machine]
	return f.Under[sid], f.As[sid], f.Side[sid]
}
