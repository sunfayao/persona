// Package rulefiles keeps the standing rule files (CLAUDE.md, AGENTS.md, GEMINI.md) of a directory from changing under
// a conversation that has already loaded them.
//
// The owner's About me notes and the reply language are part of those files. Rewriting them in the middle of a long
// conversation makes the next request read the whole conversation again without the prompt cache (and restarts Claude
// Code). So while a conversation of a directory lives, the files keep saying what that conversation loaded; a change
// reaches it once, as a one-off context block on its next message (Notice), and the files catch up when the
// conversations of the directory have all been compacted or ended (Notice, Render), or none is left. A conversation
// that starts while another lives gets the files as they are and joins the hold.
package rulefiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"sync"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// settingKey is where the book lives in the assistant's store, so a restart of the manager keeps the files as they are
// for as long as their conversations live.
const settingKey = "assistant.rule_files"

// ReceiptID names the one-off block among assistant.context's receipts: the node sends it back once the message that
// carried the block is on its way.
const ReceiptID = "rule_change"

// Values are what the rule files say that can change while a conversation runs.
type Values struct {
	Notes    string `json:"notes,omitempty"`    // About me, trimmed
	Language string `json:"language,omitempty"` // the reply language as the rules render it ("" is Chinese)
	Fixed    bool   `json:"fixed,omitempty"`    // the language is the owner's setting, not the interface's
}

func (v Values) digest() string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Store is the part of the assistant's store the book uses.
type Store interface {
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

// Dependencies of New.
type Dependencies struct {
	// Store may be nil, or return nil: the book then lives in memory only.
	Store func() Store
	// Alive says whether conversation session of machine still exists (not ended or archived). It answers true when it
	// cannot tell.
	Alive func(ctx context.Context, machine, session string) bool
	Logf  func(string, ...any)
}

// entry is one prepared directory.
type entry struct {
	// Kept is what the directory's files say; nil until known, and after Release (the next write takes the settings).
	Kept *Values `json:"kept,omitempty"`
	// Sessions are the live conversations that loaded the files, each with what it was last told about a change (Told nil:
	// nothing, it holds what Kept says) and whether it was compacted since the files were written.
	Sessions map[string]*member `json:"sessions,omitempty"`
}

// member is one live conversation of a directory.
type member struct {
	Told      *Values `json:"told,omitempty"`
	Compacted bool    `json:"compacted,omitempty"`
}

// Book is the bookkeeping of every prepared directory, keyed by machine and directory.
type Book struct {
	deps Dependencies

	mu       sync.Mutex
	loaded   bool
	dirs     map[string]*entry
	rendered map[string]Values // what Render last gave a directory: Wrote keeps it
	offered  map[string]Values // session key: the values of the block Notice last handed out

	pmu   sync.Mutex
	given map[string]Given // what each directory's node was last given (given.go)
}

// New makes an empty book; it reads the store on first use.
func New(deps Dependencies) *Book {
	if deps.Logf == nil {
		deps.Logf = func(string, ...any) {}
	}
	return &Book{deps: deps, dirs: map[string]*entry{}, rendered: map[string]Values{}, offered: map[string]Values{}, given: map[string]Given{}}
}

func dirKey(machine, dir string) string { return machine + "\x00" + dir }

// load reads the store once; b.mu is held. A failed read leaves the book unloaded, to be read again.
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
	var m map[string]*entry
	if _, err := st.Setting(ctx, settingKey, &m); err != nil {
		b.deps.Logf("assistant: rule files: read: %v", err)
		return
	}
	maps.Copy(b.dirs, m)
	b.loaded = true
}

// save writes the book; b.mu is held.
func (b *Book) save(ctx context.Context) {
	if b.deps.Store == nil {
		return
	}
	if st := b.deps.Store(); st != nil {
		if err := st.PutSetting(ctx, settingKey, b.dirs); err != nil {
			b.deps.Logf("assistant: rule files: save: %v", err)
		}
	}
}

func (b *Book) entry(key string) *entry {
	e := b.dirs[key]
	if e == nil {
		e = &entry{}
		b.dirs[key] = e
	}
	return e
}

// Render is what dir's files on machine should say: what they said when a conversation loaded them while one lives, else
// current. A conversation that ended or was archived no longer holds them (Dependencies.Alive).
func (b *Book) Render(ctx context.Context, machine, dir string, current Values) Values {
	key := dirKey(machine, dir)
	b.mu.Lock()
	b.load(ctx)
	var ids []string
	if e := b.dirs[key]; e != nil {
		ids = slices.Collect(maps.Keys(e.Sessions))
	}
	b.mu.Unlock()
	var gone []string
	for _, id := range ids { // a node call: not under the lock
		if b.deps.Alive != nil && !b.deps.Alive(ctx, machine, id) {
			gone = append(gone, id)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := current
	if e := b.dirs[key]; e != nil {
		for _, id := range gone {
			delete(e.Sessions, id)
		}
		if len(gone) > 0 {
			if e.allCompacted() {
				b.release(ctx, e) // the last conversation that held the old files has ended
			}
			b.save(ctx)
		}
		if len(e.Sessions) > 0 && e.Kept != nil {
			out = *e.Kept
		}
	}
	b.rendered[key] = out
	return out
}

// Wrote notes that dir's files on machine now say what Render last gave.
func (b *Book) Wrote(ctx context.Context, machine, dir string) {
	key := dirKey(machine, dir)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	v, ok := b.rendered[key]
	if e := b.entry(key); ok && (e.Kept == nil || *e.Kept != v) {
		e.Kept = &v
		b.save(ctx)
	}
}

// allCompacted: the directory has live conversations and every one was compacted since its files were written, so
// none still holds them.
func (e *entry) allCompacted() bool {
	for _, m := range e.Sessions {
		if !m.Compacted {
			return false
		}
	}
	return len(e.Sessions) > 0
}

// release ends the hold on e: the next write takes the settings as they are, and no conversation is owed a block about
// what its files said before; b.mu is held.
func (b *Book) release(ctx context.Context, e *entry) {
	e.Kept = nil
	for _, m := range e.Sessions {
		*m = member{}
	}
	b.save(ctx)
}

// Notice is the one-off block conversation session of dir owes for current, when its files say something else (what
// they said when it loaded them, or what a block told it before), with the receipt the node returns once it is
// delivered; with compacted the conversation counts as caught up, and the hold on the directory ends once all are. It also notes the conversation as one
// that holds the files.
func (b *Book) Notice(ctx context.Context, machine, dir, session string, current Values, compacted bool) (string, assistantwire.ViewErrorReceipt, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load(ctx)
	e := b.entry(dirKey(machine, dir))
	dirty := false
	if e.Kept == nil {
		e.Kept, dirty = &current, true
	}
	m, known := e.Sessions[session]
	if !known {
		if e.Sessions == nil {
			e.Sessions = map[string]*member{}
		}
		m, dirty = &member{}, true
		e.Sessions[session] = m
	}
	if compacted && !m.Compacted {
		m.Compacted, dirty = true, true
	}
	if dirty {
		b.save(ctx)
	}
	told := m.Told
	if told == nil {
		told = e.Kept
	}
	if compacted && e.allCompacted() { // each conversation's summary reads the files again: they take the settings
		defer b.release(ctx, e)
	}
	if *told == current {
		return "", assistantwire.ViewErrorReceipt{}, false
	}
	b.offered[dirKey(machine, session)] = current
	text := prompts.RenderAssistantRuleChange(prompts.AssistantRuleChange{Notes: current.Notes, NotesChanged: current.Notes != told.Notes,
		Language: current.Language, LanguageChanged: current.Language != told.Language || current.Fixed != told.Fixed})
	return text, assistantwire.ViewErrorReceipt{ItemID: ReceiptID, Digest: current.digest()}, true
}

// Delivered takes the block's receipt out of acks, what the node confirms of a message delivered to session: that
// conversation has been told, and is not told again until something changes. It answers the other receipts.
func (b *Book) Delivered(ctx context.Context, machine, session string, acks []assistantwire.ViewErrorReceipt) []assistantwire.ViewErrorReceipt {
	rest := acks[:0:0]
	for _, r := range acks {
		if r.ItemID != ReceiptID {
			rest = append(rest, r)
			continue
		}
		b.mu.Lock()
		b.load(ctx)
		if v, ok := b.offered[dirKey(machine, session)]; ok && v.digest() == r.Digest {
			for _, e := range b.dirs {
				if _, held := e.Sessions[session]; held {
					e.Sessions[session].Told = &v
				}
			}
			b.save(ctx)
		}
		b.mu.Unlock()
	}
	return rest
}
