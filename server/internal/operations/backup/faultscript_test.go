package backup

import (
	"context"
	"errors"
)

// Outcomes of one scripted Exists, Rename or Drop call.
const (
	callWorks       = iota
	callFails       // fails without effect
	callCommitsThen // takes effect, then reports an error (Exists: a plain failure)
	callServerDown  // fails without effect, and so does every later call
	callOutcomes
)

// scriptedCall is one call as the code saw it: what it asked and whether it
// was told the call worked (and, for Exists, the answer).
type scriptedCall struct {
	op       string // "exists", "rename", "drop"
	from, to string // the name (from) or the rename's names
	ok       bool
	exists   bool
}

// scriptedBackend fails the Exists, Rename and Drop calls it receives
// according to a script; past its end every call works (unless the server went
// down). It logs every such call for the oracle, and records every database a
// Drop removed.
type scriptedBackend struct {
	*fakeBackend
	script []int
	// decide, when set, picks each call's outcome instead of the script.
	decide  func(op, from, to string) int
	next    int
	down    bool
	calls   []scriptedCall
	dropped []string // "<name>=<dump>" of every database a Drop removed
}

func (s *scriptedBackend) outcome(op, from, to string) int {
	if s.down {
		return callServerDown
	}
	if s.decide != nil {
		return s.decide(op, from, to)
	}
	if s.next < len(s.script) {
		s.next++
		if s.script[s.next-1] == callServerDown {
			s.down = true
		}
		return s.script[s.next-1]
	}
	return callWorks
}

func (s *scriptedBackend) heal() { s.script, s.decide, s.next, s.down = nil, nil, 0, false }

func (s *scriptedBackend) Exists(ctx context.Context, target Target, name string) (bool, error) {
	if s.outcome("exists", name, "") != callWorks {
		s.calls = append(s.calls, scriptedCall{op: "exists", from: name})
		return false, errors.New("scripted: cannot check")
	}
	exists, err := s.fakeBackend.Exists(ctx, target, name)
	s.calls = append(s.calls, scriptedCall{op: "exists", from: name, ok: err == nil, exists: exists})
	return exists, err
}

func (s *scriptedBackend) Rename(ctx context.Context, target Target, from, to string) error {
	err := errors.New("scripted: rename failed")
	switch s.outcome("rename", from, to) {
	case callWorks:
		err = s.fakeBackend.Rename(ctx, target, from, to)
	case callCommitsThen:
		if err = s.fakeBackend.Rename(ctx, target, from, to); err == nil {
			err = errors.New("scripted: rename committed, then failed")
		}
	}
	s.calls = append(s.calls, scriptedCall{op: "rename", from: from, to: to, ok: err == nil})
	return err
}

func (s *scriptedBackend) Drop(ctx context.Context, target Target, name string) error {
	outcome := s.outcome("drop", name, "")
	if outcome == callWorks || outcome == callCommitsThen {
		if database := s.fakeBackend.server(target)[name]; database != nil {
			s.dropped = append(s.dropped, name+"="+database.dump)
		}
		_ = s.fakeBackend.Drop(ctx, target, name)
	}
	ok := outcome == callWorks
	s.calls = append(s.calls, scriptedCall{op: "drop", from: name, ok: ok})
	if !ok {
		return errors.New("scripted: drop failed")
	}
	return nil
}
