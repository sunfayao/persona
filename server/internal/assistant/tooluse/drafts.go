package tooluse

import (
	"context"
	"errors"
	"slices"
	"time"
)

// The kinds of a draft.
const (
	// DraftNote: the friction is how the tool was used (a retry with other arguments worked): a field note would help.
	DraftNote = "note"
	// DraftFix: the friction looks like a defect of Persona's own: a fix belongs in the code or the tool's prompt.
	DraftFix = "fix"
)

// The states of a draft.
const (
	DraftPending   = "pending"   // waits for the main conversation or the user
	DraftProposed  = "proposed"  // a field note was proposed from it (NoteID)
	DraftDismissed = "dismissed" // the main conversation or the user set it aside
	DraftFiled     = "filed"     // a to-do was made of it (TodoID)
)

// MaxEvidence bounds the conversations a draft names.
const MaxEvidence = 5

// Draft is what the friction loop proposes from repeated friction of one tool.
type Draft struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // DraftNote or DraftFix
	Tool    string `json:"tool"`
	Code    string `json:"code"`
	Runtime string `json:"runtime,omitempty"`
	// Count is how many failed calls it stands for, between FirstAt and LastAt.
	Count   int       `json:"count"`
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
	// Skeleton is the failed calls' argument skeleton; RetrySkeleton the one of the retry that worked, if any.
	Skeleton      string `json:"skeleton"`
	RetrySkeleton string `json:"retry_skeleton,omitempty"`
	// Evidence are the conversations it happened in, newest first, at most MaxEvidence.
	Evidence []Evidence `json:"evidence,omitempty"`
	// Text is the draft itself: a field note of at most 200 characters, or what a fix would change.
	Text      string    `json:"text"`
	Status    string    `json:"status"`
	NoteID    string    `json:"note_id,omitempty"`
	TodoID    string    `json:"todo_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Evidence is one conversation a draft's friction happened in.
type Evidence struct {
	Machine string    `json:"machine"`
	Session string    `json:"session"`
	At      time.Time `json:"at"`
}

type draftsDoc struct {
	Drafts []Draft `json:"drafts"`
}

// ErrNoDraft: no draft has that id.
var ErrNoDraft = errors.New("no such draft")

// Drafts are the stored drafts, in the order they were made.
func (b *Book) Drafts(ctx context.Context) ([]Draft, error) {
	b.draftsMu.Lock()
	defer b.draftsMu.Unlock()
	return b.loadDrafts(ctx)
}

func (b *Book) loadDrafts(ctx context.Context) ([]Draft, error) {
	if b.h.Load == nil {
		return nil, nil
	}
	var doc draftsDoc
	if _, err := b.h.Load(ctx, DraftsKey, &doc); err != nil {
		return nil, err
	}
	return doc.Drafts, nil
}

// PutDrafts replaces the stored drafts (the friction loop's whole list).
func (b *Book) PutDrafts(ctx context.Context, drafts []Draft) error {
	b.draftsMu.Lock()
	defer b.draftsMu.Unlock()
	return b.saveDrafts(ctx, drafts)
}

func (b *Book) saveDrafts(ctx context.Context, drafts []Draft) error {
	if b.h.Save == nil {
		return nil
	}
	return b.h.Save(ctx, DraftsKey, draftsDoc{Drafts: drafts})
}

// UpdateDraft changes one draft through f and stores it; ErrNoDraft when there is none with that id. f returning an
// error changes nothing.
func (b *Book) UpdateDraft(ctx context.Context, id string, f func(*Draft) error) error {
	b.draftsMu.Lock()
	defer b.draftsMu.Unlock()
	drafts, err := b.loadDrafts(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(drafts, func(d Draft) bool { return d.ID == id })
	if i < 0 {
		return ErrNoDraft
	}
	d := drafts[i]
	if err := f(&d); err != nil {
		return err
	}
	d.UpdatedAt = b.now().UTC()
	drafts[i] = d
	return b.saveDrafts(ctx, drafts)
}
