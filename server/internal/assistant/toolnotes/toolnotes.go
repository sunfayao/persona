// Package toolnotes keeps the field notes: one-line tips about using a tool, learned in the main conversation and
// appended to that tool's description in its tool list once the user approves each on a one-tap card. A note may hold
// on one computer or for one runtime only; after ReviewDays it is due for review and leaves the descriptions until it
// is proposed again; it retires itself when its tool's description changes (a release that fixed what the note worked
// around). The tool record's friction drafts (assistant/tooluse) are proposed as notes from here, set aside, or, for a
// defect of Persona's own, filed as a to-do whose completion retires the notes it covers. The Service lends what the
// package needs through Host.
package toolnotes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/approval"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Key is where the notes are kept, in the assistant's settings.
const Key = "assistant.tool_notes"

// ToolName is the tool that proposes and reviews the notes.
const ToolName = "tool_note"

// The states of a note.
const (
	StatusProposed = "proposed" // its card waits for the user's tap; not in any description
	StatusApproved = "approved" // in its tool's description until ReviewAt
	StatusRetired  = "retired"  // out for good (RetiredReason says why)
)

// The kinds and states of a friction draft, as the tool record keeps them (assistant/tooluse).
const (
	DraftNote      = "note"
	DraftFix       = "fix"
	DraftPending   = "pending"
	DraftProposed  = "proposed"
	DraftDismissed = "dismissed"
	DraftFiled     = "filed"
)

const (
	// MaxText bounds a note, in characters; it is one line.
	MaxText = 200
	// MaxPerTool and MaxBlock bound what one tool's description gains: the newest notes that fit, at most this many
	// and this many bytes of "Field notes:" block, so Antigravity's tool budget is not spent on notes.
	MaxPerTool = 3
	MaxBlock   = 600
	// ReviewDays is how long an approved note stays before it is due for review.
	ReviewDays = 90
	// maxActive bounds the notes that are not retired; maxRetired and keepRetired the retired ones kept as a record.
	maxActive   = 100
	maxRetired  = 100
	keepRetired = 90 * 24 * time.Hour
	// PersonaRepository is the repository a fix of Persona's own is filed against, when it is saved in Persona.
	PersonaRepository = "sunfayao/persona"
)

// Note is one field note.
type Note struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	// Machine is the id of the computer it holds on; "" for every computer.
	Machine string `json:"machine,omitempty"`
	// Runtime: only conversations on this runtime (claude, codex, agy) get it; "" for every runtime.
	Runtime string `json:"runtime,omitempty"`
	Text    string `json:"text"`
	// Evidence is what showed it, from its friction draft.
	Evidence string `json:"evidence,omitempty"`
	// By is the conversation that proposed it.
	By string `json:"by"`
	// PromptDigest is Digest of its tool's description when it was proposed: a description that changed since retires it.
	PromptDigest string    `json:"prompt_digest"`
	CreatedAt    time.Time `json:"created_at"`
	ApprovedAt   time.Time `json:"approved_at,omitzero"`
	// ReviewAt: from then on the note is due for review and out of the descriptions until proposed again.
	ReviewAt      time.Time `json:"review_at"`
	Status        string    `json:"status"`
	RetiredAt     time.Time `json:"retired_at,omitzero"`
	RetiredReason string    `json:"retired_reason,omitempty"`
	// DraftID and Code: the friction draft it was proposed from, and that draft's failure code.
	DraftID string `json:"draft_id,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Fix is a fix draft filed as a to-do: once the to-do is done, the notes of the same tool and code retire as covered.
type Fix struct {
	DraftID   string    `json:"draft_id"`
	TodoID    string    `json:"todo_id"`
	Tool      string    `json:"tool"`
	Code      string    `json:"code"`
	FiledAt   time.Time `json:"filed_at"`
	CoveredAt time.Time `json:"covered_at,omitzero"`
}

type doc struct {
	Notes []Note `json:"notes"`
	Fixes []Fix  `json:"fixes,omitempty"`
}

func (d doc) clone() doc {
	return doc{Notes: slices.Clone(d.Notes), Fixes: slices.Clone(d.Fixes)}
}

func (d doc) find(id string) int {
	return slices.IndexFunc(d.Notes, func(n Note) bool { return n.ID == id })
}

// Draft is a friction draft of the tool record (assistant/tooluse Draft), as the root hands it over.
type Draft struct {
	ID, Kind, Tool, Code, Runtime string
	Count                         int
	FirstAt, LastAt               time.Time
	Skeleton, RetrySkeleton       string
	Evidence                      []Evidence
	Text, Status, NoteID, TodoID  string
}

// Evidence is a conversation a draft's friction happened in.
type Evidence struct {
	Machine, Session string
	At               time.Time
}

// ErrNoDraft: no friction draft has that id.
var ErrNoDraft = errors.New("no such friction draft")

// Call is the conversation a tool_note call comes from, and its user's language and time zone.
type Call struct {
	Machine, Session string
	Lang             i18n.Lang
	Zone             *time.Location
}

// Card is the one-tap card a proposal waits on: the conversation it is shown in, its line in the user's language, the
// details it shows, and Key, which makes two proposals in one conversation the same one
// (approval.Operation.Replacement).
type Card struct {
	Machine, Session string
	Summary          string
	Shown            json.RawMessage
	Key              string
}

// Repository is a repository saved in Persona, as a new to-do is bound to one: its id and the catalog's revision.
type Repository struct {
	ID       string
	Revision int64
}

// Host is what the package takes from the Service. Every function is required.
type Host struct {
	// Load reads the stored value of key into out (false when there is none); Save stores v under key.
	Load func(ctx context.Context, key string, out any) (bool, error)
	Save func(ctx context.Context, key string, v any) error
	Now  func() time.Time
	Logf func(format string, args ...any)

	// Tools are the tools in the main conversation's list now, built-in and the extensions', as the list shows them,
	// without notes.
	Tools func(ctx context.Context) []proto.Tool
	// Runtime is the runtime the main conversation runs on (claude, codex, agy; "" when unknown).
	Runtime func(ctx context.Context) string
	// Machine finds the computer a call names by its id or name: its id and its name; false when the user has none.
	Machine func(ctx context.Context, name string) (id, display string, ok bool)
	// MachineName is computer id's name, which a note that holds on it says.
	MachineName func(id string) string
	// Private are the names no note may carry: the user's computers' names and ids, and host names.
	Private func(ctx context.Context) []string
	// Drafts are the friction drafts. UpdateDraft changes one through f and stores it (ErrNoDraft when there is none;
	// f's error changes nothing).
	Drafts      func(ctx context.Context) ([]Draft, error)
	UpdateDraft func(ctx context.Context, id string, f func(*Draft) error) error
	// Confirm shows card and waits as a prepared approval does: op.Apply runs only on the user's own Allow, and
	// op.Discard once the card has ended either way.
	Confirm func(ctx context.Context, card Card, op approval.Operation) (approval.Outcome, error)
	// Repository is the repository saved in Persona at path (owner/name); false when there is none.
	Repository func(ctx context.Context, path string) (Repository, bool)
	// FileTodo makes the to-do of a fix the way todo_create does, bound to repo when it is not nil, or finds the open
	// one of the same title: its id, and whether it was there already.
	FileTodo func(ctx context.Context, c Call, title, notes string, repo *Repository) (id string, existed bool, err error)
	// TodoDone: to-do id is done.
	TodoDone func(id string) bool
	// Links are links to these conversations, one each ("" for one without).
	Links func(ctx context.Context, conversations []Evidence) []string
	// Changed: the notes changed; rendered when what the conversations' tool lists show changed with them (the tool
	// watches are woken), else only what waits for review.
	Changed func(rendered bool)
}

// Domain is the field notes. The stored notes are held in memory after the first read: the tool lists read them on
// every list and every watch's heartbeat, and every change goes through here.
type Domain struct {
	h Host

	// writeMu orders the read-modify-writes of the stored notes; mu guards the fields below it and is never held
	// across the store, so the tool lists never wait for a write.
	writeMu sync.Mutex
	mu      sync.Mutex
	loaded  bool
	doc     doc
	live    map[string]bool // the proposals whose card this process holds open
	swept   time.Time       // the clock at the last Sweep
}

// New makes the field notes; the stored ones are read at the first use.
func New(h Host) *Domain {
	return &Domain{h: h, live: map[string]bool{}}
}

func (d *Domain) now() time.Time { return d.h.Now().UTC() }

// ensure reads the stored notes once; a failed read is tried again at the next use.
func (d *Domain) ensure(ctx context.Context) error {
	d.mu.Lock()
	loaded := d.loaded
	d.mu.Unlock()
	if loaded {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	loaded = d.loaded
	d.mu.Unlock()
	if loaded {
		return nil
	}
	var stored doc
	if _, err := d.h.Load(ctx, Key, &stored); err != nil {
		return err
	}
	d.mu.Lock()
	d.doc, d.loaded = stored, true
	d.mu.Unlock()
	return nil
}

// held is a copy of the notes held (none while they cannot be read).
func (d *Domain) held(ctx context.Context) doc {
	if err := d.ensure(ctx); err != nil {
		d.h.Logf("tool notes: read: %v", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.doc.clone()
}

// change applies f to a copy of the notes and stores the copy when f says it changed it; the held notes become the
// copy only once it is stored. f's error is returned as it is.
func (d *Domain) change(ctx context.Context, f func(*doc) (bool, error)) error {
	if err := d.ensure(ctx); err != nil {
		return err
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	next := d.doc.clone()
	d.mu.Unlock()
	changed, err := f(&next)
	if err != nil || !changed {
		return err
	}
	if err := d.h.Save(ctx, Key, next); err != nil {
		return err
	}
	d.mu.Lock()
	d.doc = next
	d.mu.Unlock()
	return nil
}

func (d *Domain) setLive(id string, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.live[id] = true
	} else {
		delete(d.live, id)
	}
}

func (d *Domain) isLive(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.live[id]
}

// shownAt: n is in its tool's description at now (approved, before its review date), runtime and digest aside.
func (n Note) shownAt(now time.Time) bool {
	return n.Status == StatusApproved && n.ReviewAt.After(now)
}

// retire takes n out for good, saying why.
func retire(n *Note, now time.Time, reason string) {
	n.Status, n.RetiredAt, n.RetiredReason = StatusRetired, now, reason
}

// newID is a note's id: "tn_" and 8 random hex digits.
func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "tn_" + hex.EncodeToString(b)
}
