package toolnotes

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// The reasons a note retires by itself.
const (
	reasonDeclined = "declined by the user"
	reasonEnded    = "not approved: its card ended (%s)"
	reasonRestart  = "not approved: its card ended when Persona restarted"
	reasonChanged  = "its tool's description changed (a new release): it may no longer hold; propose it again if it does"
	reasonCovered  = "covered: its fix, to-do %s, is done"
)

// Sweep is the notes' upkeep, run every few minutes (the tool record's tick): a proposal whose card is gone (the
// manager restarted) retires unanswered; a note whose tool's description changed retires; a done fix to-do retires the
// notes of its tool and code; retired notes past keepRetired, or past maxRetired, are dropped. What changed is stored
// once, and the tool watches are woken once when what the descriptions show changed, a note past its review date since
// the last Sweep included. A tool missing from the list (an extension off or still starting) retires none of its notes.
func (d *Domain) Sweep(ctx context.Context) {
	if err := d.ensure(ctx); err != nil {
		d.h.Logf("tool notes: read: %v", err)
		return
	}
	now := d.now()
	held := d.held(ctx)
	// What the notes are checked against is read before the write lock is taken: the tools (an extension's answer
	// may take a while) and the fixes' to-dos.
	digests := map[string]string{}
	if slices.ContainsFunc(held.Notes, func(n Note) bool { return n.Status != StatusRetired }) {
		for _, t := range d.h.Tools(ctx) {
			digests[t.Name] = Digest(t)
		}
	}
	covered := map[string]string{} // tool and code → the done to-do that covers them
	for _, f := range held.Fixes {
		if f.CoveredAt.IsZero() && d.h.TodoDone(f.TodoID) {
			covered[f.Tool+"\x00"+f.Code] = f.TodoID
		}
	}
	rendered := false
	err := d.change(ctx, func(doc *doc) (bool, error) {
		changed := false
		for i := range doc.Notes {
			n := &doc.Notes[i]
			if n.Status == StatusRetired {
				continue
			}
			digest, listed := digests[n.Tool]
			todo, done := covered[n.Tool+"\x00"+n.Code]
			switch {
			case n.Status == StatusProposed && !d.isLive(n.ID):
				retire(n, now, reasonRestart)
			case listed && digest != n.PromptDigest:
				rendered = rendered || n.shownAt(now)
				retire(n, now, reasonChanged)
			case n.Code != "" && done:
				rendered = rendered || n.shownAt(now)
				retire(n, now, fmt.Sprintf(reasonCovered, todo))
			default:
				continue
			}
			changed = true
		}
		for i := range doc.Fixes {
			if f := &doc.Fixes[i]; f.CoveredAt.IsZero() && covered[f.Tool+"\x00"+f.Code] == f.TodoID {
				f.CoveredAt, changed = now, true
			}
		}
		return prune(doc, now) || changed, nil
	})
	if err != nil {
		d.h.Logf("tool notes: sweep: %v", err)
		return
	}
	d.mu.Lock()
	last := d.swept
	d.swept = now
	for _, n := range d.doc.Notes {
		if n.Status == StatusApproved && n.ReviewAt.After(last) && !n.ReviewAt.After(now) {
			rendered = true // due for review since the last Sweep: out of the descriptions now
		}
	}
	d.mu.Unlock()
	if rendered {
		d.h.Changed(true)
	}
}

// prune drops the notes retired longer ago than keepRetired and the oldest retired ones past maxRetired, and the fixes
// covered longer ago than keepRetired. True when it dropped any.
func prune(doc *doc, now time.Time) bool {
	before := len(doc.Notes) + len(doc.Fixes)
	doc.Notes = slices.DeleteFunc(doc.Notes, func(n Note) bool {
		return n.Status == StatusRetired && now.Sub(n.RetiredAt) > keepRetired
	})
	var retired []Note
	for _, n := range doc.Notes {
		if n.Status == StatusRetired {
			retired = append(retired, n)
		}
	}
	if extra := len(retired) - maxRetired; extra > 0 {
		slices.SortFunc(retired, func(a, b Note) int { return a.RetiredAt.Compare(b.RetiredAt) })
		drop := map[string]bool{}
		for _, n := range retired[:extra] {
			drop[n.ID] = true
		}
		doc.Notes = slices.DeleteFunc(doc.Notes, func(n Note) bool { return drop[n.ID] })
	}
	doc.Fixes = slices.DeleteFunc(doc.Fixes, func(f Fix) bool {
		return !f.CoveredAt.IsZero() && now.Sub(f.CoveredAt) > keepRetired
	})
	return len(doc.Notes)+len(doc.Fixes) != before
}

// Waiting is what waits for review in tool_note list, for the main conversation's snapshot: the pending friction
// drafts, and the notes past their review date.
func (d *Domain) Waiting(ctx context.Context) (drafts, due int) {
	if list, err := d.h.Drafts(ctx); err != nil {
		d.h.Logf("tool notes: drafts: %v", err)
	} else {
		for _, x := range list {
			if x.Status == DraftPending {
				drafts++
			}
		}
	}
	now := d.now()
	for _, n := range d.held(ctx).Notes {
		if n.Status == StatusApproved && !n.ReviewAt.After(now) {
			due++
		}
	}
	return drafts, due
}
