// Package supervisionpolicy decides which conversations carry the supervision policy in their standing text and which
// are told it once, with the first signal they are asked to judge.
//
// The policy is long, and the main conversation carries it on every turn, the owner's own included, although most of
// its days it judges nothing: a supervisor Agent is attached to what it dispatches. With the setting
// assistant.todo_supervisor on auto, a conversation that supervises no open to-do itself when it starts gets only a short
// supervision section in its rules, and the policy reaches it as a one-off context block with the message that first
// asks it to judge a signal: told once, again only when the node did not confirm it, and again after a compaction.
//
// The decision is made once per start (and once more after a compaction) and kept, so the standing text of a live
// conversation never gains or loses the policy under it: that would make its model read the whole context again
// without the prompt cache. The book is kept in the store, so a restart of the manager does not undo a decision.
package supervisionpolicy

import (
	"context"
	"maps"
	"sync"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// ReceiptID names the one-off block among assistant.context's receipts.
const ReceiptID = "supervision_policy"

const settingKey = "assistant.supervision_policy"

// Store is the part of the assistant's store the book uses.
type Store interface {
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

// Dependencies of New.
type Dependencies struct {
	// Store may be nil, or return nil: the book then lives in memory only.
	Store func() Store
	Logf  func(string, ...any)
}

// state is one conversation's.
type state struct {
	// Without: its standing text carries no policy.
	Without bool `json:"without,omitempty"`
	// Told: the policy block reached it since it started (or was compacted).
	Told bool `json:"told,omitempty"`
	// Stale: it was compacted, so its next start decides again.
	Stale bool `json:"stale,omitempty"`
}

// Book is every conversation's state, by machine and session.
type Book struct {
	deps Dependencies

	mu     sync.Mutex
	loaded bool
	list   map[string]*state
}

func New(deps Dependencies) *Book {
	if deps.Logf == nil {
		deps.Logf = func(string, ...any) {}
	}
	return &Book{deps: deps, list: map[string]*state{}}
}

func key(machine, session string) string { return machine + "\x00" + session }

func (b *Book) load(ctx context.Context) {
	if b.loaded {
		return
	}
	var st Store
	if b.deps.Store != nil {
		st = b.deps.Store()
	}
	if st == nil {
		b.loaded = true
		return
	}
	var m map[string]*state
	if _, err := st.Setting(ctx, settingKey, &m); err != nil {
		b.deps.Logf("assistant: supervision policy: read: %v", err)
		return
	}
	maps.Copy(b.list, m)
	b.loaded = true
}

func (b *Book) save(ctx context.Context) {
	if b.deps.Store == nil {
		return
	}
	if st := b.deps.Store(); st != nil {
		if err := st.PutSetting(ctx, settingKey, b.list); err != nil {
			b.deps.Logf("assistant: supervision policy: save: %v", err)
		}
	}
}

// Started records how conversation session was started: without the policy in its standing text, or with it. A
// conversation that was not started since this book exists (one that is already live) has no record and keeps the
// policy it has.
func (b *Book) Started(ctx context.Context, machine, session string, without bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	b.list[key(machine, session)] = &state{Without: without}
	b.save(ctx)
}

// Standing says whether conversation session's standing text carries the policy. One with no record keeps it; one
// compacted since its start is decided again by supervises (whether it supervises an open to-do itself), called outside
// the book's lock; the rest keep what they were started with, so the standing text of a live conversation never gains or
// loses the policy under it.
func (b *Book) Standing(ctx context.Context, machine, session string, supervises func() (bool, error)) (bool, error) {
	k := key(machine, session)
	b.mu.Lock()
	b.load(ctx)
	st := b.list[k]
	b.mu.Unlock()
	if st == nil || !st.Stale {
		return st == nil || !st.Without, nil
	}
	own, err := supervises()
	if err != nil {
		return true, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := &state{Without: !own}
	if cur := b.list[k]; cur != nil && !own {
		next.Told = cur.Told // told after its compaction: it has the policy in its context already
	}
	b.list[k] = next
	b.save(ctx)
	return own, nil
}

// Notice is the one-off policy block conversation session is owed with a message that asks it to judge, with the
// receipt the node returns: only a conversation whose standing text carries none, and that was not told since it started
// or was compacted.
func (b *Book) Notice(ctx context.Context, machine, session, text string) (string, assistantwire.ViewErrorReceipt, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	st := b.list[key(machine, session)]
	if st == nil || !st.Without || st.Told {
		return "", assistantwire.ViewErrorReceipt{}, false
	}
	return text, assistantwire.ViewErrorReceipt{ItemID: ReceiptID, Digest: "1"}, true
}

// Delivered takes the block's receipt out of acks, what the node confirms of a message delivered to session: the
// conversation has the policy now. It answers the other receipts.
func (b *Book) Delivered(ctx context.Context, machine, session string, acks []assistantwire.ViewErrorReceipt) []assistantwire.ViewErrorReceipt {
	rest := acks[:0:0]
	for _, r := range acks {
		if r.ItemID != ReceiptID {
			rest = append(rest, r)
			continue
		}
		b.mu.Lock()
		b.load(ctx)
		if st := b.list[key(machine, session)]; st != nil && !st.Told {
			st.Told = true
			b.save(ctx)
		}
		b.mu.Unlock()
	}
	return rest
}

// Compacted notes that conversation session was compacted: what it was told is summarized away, and its next start
// decides again.
func (b *Book) Compacted(ctx context.Context, machine, session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	k := key(machine, session)
	if st := b.list[k]; st == nil || st.Told || !st.Stale {
		b.list[k] = &state{Stale: true, Without: st != nil && st.Without}
		b.save(ctx)
	}
}

// Forget drops conversation session (it ended).
func (b *Book) Forget(ctx context.Context, machine, session string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	if _, ok := b.list[key(machine, session)]; ok {
		delete(b.list, key(machine, session))
		b.save(ctx)
	}
}
