// Package ownerfence orders owner input and supervisory actions on managed sessions.
package ownerfence

import (
	"sync"
	"time"
)

type Ref struct{ Machine, Session string }

type Input struct {
	Seq         uint64
	At          time.Time
	Text        string
	ClientMsgID string
	RelayOf     uint64 // the owner input in main this write relays
	ByOwner     bool
}

// Book owns the manager's input and observation watermarks. A zero Book is ready to use.
type Book struct {
	mu       sync.Mutex
	seq      uint64
	inputs   map[Ref]Input
	direct   map[Ref]time.Time // when the owner last typed in, or answered a card of, the session themselves
	observed map[Ref]map[Ref]uint64
}

func (b *Book) record(target Ref, input Input) Input {
	if b.inputs == nil {
		b.inputs = make(map[Ref]Input)
	}
	b.seq++
	input.Seq = b.seq
	b.inputs[target] = input
	if input.ByOwner && input.RelayOf == 0 {
		if b.direct == nil {
			b.direct = make(map[Ref]time.Time)
		}
		b.direct[target] = input.At
	}
	return input
}

// LastDirect is when the owner last gave target input of their own (not one relayed from the main conversation); zero
// when this manager has seen none.
func (b *Book) LastDirect(target Ref) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.direct[target]
}

func (b *Book) Record(target Ref, input Input) Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.record(target, input)
}

func (b *Book) Latest(target Ref) Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inputs[target]
}

// Observe records exactly the input shown to the supervisor. An input that arrives while the result is being built
// still compares newer. Concurrent reads completing out of order cannot roll back an observation.
func (b *Book) Observe(supervisor, target Ref, input Input) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observe(supervisor, target, input.Seq)
}

func (b *Book) observe(supervisor, target Ref, seq uint64) {
	if seq <= b.observed[supervisor][target] {
		return
	}
	if b.observed == nil {
		b.observed = make(map[Ref]map[Ref]uint64)
	}
	if b.observed[supervisor] == nil {
		b.observed[supervisor] = make(map[Ref]uint64)
	}
	b.observed[supervisor][target] = seq
}

type Action struct {
	Supervisor, Target Ref
	Observed           uint64
	// Owner is the input verified in the main conversation's current transcript.
	Owner Input
}

func (b *Book) Action(supervisor, target Ref, askedAt ...time.Time) Action {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := Action{Supervisor: supervisor, Target: target, Observed: b.observed[supervisor][target]}
	// An explicitly addressed card opened after the latest input is a new question. Its answer does not advance
	// the supervisor's general-purpose observation of the worker.
	if len(askedAt) > 0 {
		if input := b.inputs[target]; !input.At.After(askedAt[0]) {
			a.Observed = max(a.Observed, input.Seq)
		}
	}
	return a
}

func (b *Book) Check(a Action) Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.check(a)
}

func (b *Book) check(a Action) Input {
	cur := b.inputs[a.Target]
	if cur.Seq > max(a.Observed, a.Owner.Seq) && (cur.RelayOf == 0 || cur.RelayOf != a.Owner.Seq) {
		return cur
	}
	return Input{}
}

// Admit is the linearization point before delivery. An instruction expressly relayed from the owner's main turn
// advances the worker's watermark and spends that authority for this call's competing supervisors.
func (b *Book) Admit(a Action, relayed Input) Input {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur := b.check(a); cur.Seq != 0 {
		return cur
	}
	if relayed.Text != "" {
		input := b.record(a.Target, relayed)
		b.observe(a.Supervisor, a.Target, input.Seq)
	}
	return Input{}
}
