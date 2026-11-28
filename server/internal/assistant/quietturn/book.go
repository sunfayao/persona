// Package quietturn keeps the state that decides how a supervision turn may end: which delivered supervision
// batches are still open (so supervision_finish can hide or fold the right one), and which conversations hold their
// automatic pushes while a supervision turn runs.
package quietturn

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// StoreKey is the settings key the open batches are kept under, so a manager restart does not lose them.
const StoreKey = "assistant.supervision_batches"

// DefaultMax is how long a batch or a push hold lasts when Dependencies.Max is not set.
const DefaultMax = 2 * time.Hour

// ErrNoBatch: the conversation has no open supervision batch, or not the one asked for.
var ErrNoBatch = errors.New("there is no open supervision batch in this conversation (or not that one): this turn was not started by a supervision message, so just reply normally")

// Store is the settings store the batches are kept in.
type Store interface {
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

// Clock tells the time.
type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Dependencies of New. Store may return nil (no store: the batches live in memory only).
type Dependencies struct {
	Store func() Store
	Clock Clock
	Max   time.Duration
	Logf  func(string, ...any)
}

type batch struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Visible bool      `json:"visible,omitempty"` // a card, a push or a question already made the turn visible
}

type state struct {
	Open map[string][]batch `json:"open,omitempty"` // by conversation, oldest first
}

// Book is the state of the supervision batches and push holds of all conversations.
type Book struct {
	store func() Store
	clock Clock
	max   time.Duration
	logf  func(string, ...any)

	mu    sync.Mutex // the push holds; never held across the store
	held  map[string]time.Time
	shown map[string]bool

	bmu    sync.Mutex // the batches and their persistence
	loaded bool
	open   map[string][]batch
}

// New makes a book with nothing open and nothing held.
func New(d Dependencies) *Book {
	b := &Book{store: d.Store, clock: d.Clock, max: d.Max, logf: d.Logf, held: map[string]time.Time{}, shown: map[string]bool{}}
	if b.clock == nil {
		b.clock = realClock{}
	}
	if b.max <= 0 {
		b.max = DefaultMax
	}
	if b.logf == nil {
		b.logf = func(string, ...any) {}
	}
	return b
}

// Hold: a supervision message went into conversation id, so its turn's normal finish is not pushed.
func (b *Book) Hold(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.held[id] = b.clock.Now()
	delete(b.shown, id)
}

// Release: someone else speaks in id. It says whether the conversation was held. Open batches stay open.
func (b *Book) Release(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, was := b.held[id]
	delete(b.held, id)
	delete(b.shown, id)
	return was
}

// Held: the current turn of conversation id was started by a supervision message.
func (b *Book) Held(id string) bool {
	_, ok := b.HeldSince(id)
	return ok
}

// HeldSince: when the hold of conversation id began.
func (b *Book) HeldSince(id string) (time.Time, bool) {
	if b == nil {
		return time.Time{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	at, ok := b.held[id]
	return at, ok
}

// Shown: a card, a notification or a question made the held turn visible.
func (b *Book) Shown(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.shown[id]
}

// HeldIDs lists the held conversations.
func (b *Book) HeldIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.held))
	for id := range b.held {
		out = append(out, id)
	}
	return out
}

// Open records that supervision batch id was delivered to conversation conv. A batch already open stays as it is.
func (b *Book) Open(ctx context.Context, conv, id string) {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	if !b.load(ctx) || slices.ContainsFunc(b.open[conv], func(x batch) bool { return x.ID == id }) {
		return
	}
	b.open[conv] = append(b.open[conv], batch{ID: id, At: b.clock.Now()})
	b.save(ctx)
}

// Drop forgets batch id of conv, which turned out never to have been delivered.
func (b *Book) Drop(ctx context.Context, conv, id string) {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	if !b.load(ctx) {
		return
	}
	if list := slices.DeleteFunc(slices.Clone(b.open[conv]), func(x batch) bool { return x.ID == id }); len(list) != len(b.open[conv]) {
		b.set(conv, list)
		b.save(ctx)
	}
}

// Promote: a card, a notification or a question made the turn visible. The conversation's push flag is set while it
// is held, and the oldest open batch is marked, so a later silent finish of it cannot hide what the user saw.
func (b *Book) Promote(ctx context.Context, conv string) {
	b.mu.Lock()
	if _, ok := b.held[conv]; ok {
		b.shown[conv] = true
	}
	b.mu.Unlock()
	b.bmu.Lock()
	defer b.bmu.Unlock()
	if !b.load(ctx) || len(b.open[conv]) == 0 || b.open[conv][0].Visible {
		return
	}
	b.open[conv][0].Visible = true
	b.save(ctx)
}

// Receipt is what a finish reports: the level the turn ended at, and a note when it differs from what was asked.
type Receipt struct {
	Level string // "triage" or "user"
	Note  string
}

// Finish closes batch of conv (the oldest open one when batch is empty) and every older open batch. A silent finish
// of a batch that was already made visible is not an error: it reports level "user" with a note. A finish with
// visibility "user" also marks the held conversation shown.
func (b *Book) Finish(ctx context.Context, conv, batchID, visibility string) (Receipt, error) {
	b.bmu.Lock()
	defer b.bmu.Unlock()
	b.load(ctx)
	list := b.open[conv]
	at := 0
	if batchID != "" {
		at = slices.IndexFunc(list, func(x batch) bool { return x.ID == batchID })
	}
	if at < 0 || len(list) == 0 {
		return Receipt{}, ErrNoBatch
	}
	r := Receipt{Level: "triage"}
	switch {
	case visibility == "user":
		r.Level = "user"
		b.mu.Lock()
		if _, ok := b.held[conv]; ok {
			b.shown[conv] = true
		}
		b.mu.Unlock()
	case slices.ContainsFunc(list[:at+1], func(x batch) bool { return x.Visible }):
		r.Level, r.Note = "user", "this turn is already visible through its card or question; it stays visible"
	}
	b.set(conv, slices.Clone(list[at+1:]))
	b.save(ctx)
	return r, nil
}

// Expire drops the holds and the batches older than the maximum. It says whether a hold went.
func (b *Book) Expire(ctx context.Context) bool {
	now, changed := b.clock.Now(), false
	b.mu.Lock()
	for id, at := range b.held {
		if now.Sub(at) > b.max {
			delete(b.held, id)
			delete(b.shown, id)
			changed = true
		}
	}
	b.mu.Unlock()
	b.bmu.Lock()
	defer b.bmu.Unlock()
	if !b.load(ctx) {
		return changed
	}
	dirty := false
	for conv, list := range b.open {
		if kept := slices.DeleteFunc(slices.Clone(list), func(x batch) bool { return now.Sub(x.At) > b.max }); len(kept) != len(list) {
			b.set(conv, kept)
			dirty = true
		}
	}
	if dirty {
		b.save(ctx)
	}
	return changed
}

func (b *Book) set(conv string, list []batch) {
	if len(list) == 0 {
		delete(b.open, conv)
		return
	}
	b.open[conv] = list
}

// load reads the stored batches once; false while the store cannot be read. Under bmu.
func (b *Book) load(ctx context.Context) bool {
	if b.loaded {
		return true
	}
	st := state{}
	if s := b.storage(); s != nil {
		if _, err := s.Setting(ctx, StoreKey, &st); err != nil {
			b.logf("assistant: supervision batches: load: %v", err)
			return false
		}
	}
	if st.Open == nil {
		st.Open = map[string][]batch{}
	}
	b.open, b.loaded = st.Open, true
	return true
}

func (b *Book) save(ctx context.Context) {
	if s := b.storage(); s != nil {
		if err := s.PutSetting(ctx, StoreKey, state{Open: b.open}); err != nil {
			b.logf("assistant: supervision batches: save: %v", err)
		}
	}
}

func (b *Book) storage() Store {
	if b.store == nil {
		return nil
	}
	return b.store()
}
