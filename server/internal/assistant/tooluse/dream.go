package tooluse

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/prompts"
)

// The friction dream: now and then (DreamEvery at most, and only when a failure has come since it last ran) the failed
// calls of the last Window are grouped by tool, code and argument skeleton, and a group that repeats becomes a draft, a
// field note or a fix suggestion, merged with the drafts stored before (DreamDrafts). It only writes drafts: it never
// interrupts a conversation, tells anyone or calls a model, and a draft's text is a template filled in.

const (
	// DreamKey stores when the friction dream last ran.
	DreamKey = "assistant.tool_friction_dream"
	// DreamEvery is how often the friction dream runs at most. Drafts wait for the main conversation or the user, so a
	// few hours' delay costs nothing, and the stored drafts are not rewritten after every failure.
	DreamEvery = 6 * time.Hour
	// MaxDrafts bounds the drafts kept; the ones whose friction was seen longest ago go first.
	MaxDrafts = 200
	// noteMax is the longest field note, fixMax the longest fix suggestion, in characters.
	noteMax = 200
	fixMax  = 400
)

// A group of failures becomes a draft once it repeats: draftMin failures, or draftMinPaired when a retry with other
// arguments worked after one of them. One failure says little: a typo, a busy computer, a state that changed under the
// call. Two that a retry cured show that the mistake is made again and what avoids it, which is all a field note needs.
// Without a cure the model may have been unlucky twice, and a fix is a claim about Persona's own code: the third
// failure tells a pattern from chance.
const (
	draftMin       = 3
	draftMinPaired = 2
)

// dreamState is what the Book knows of the friction dream's last run, read from DreamKey once.
type dreamState struct {
	loaded bool
	mark   dreamMark
}

type dreamMark struct {
	// At is when the friction dream last ran; Through the newest failure it took in.
	At      time.Time `json:"at"`
	Through time.Time `json:"through"`
}

// Dream runs the friction dream on the failures of the last Window (DreamDrafts) and stores the drafts, when DreamEvery
// has passed since it last ran and a failure has come since. A failure is taken in once RetryWindow has passed, when
// whether a retry cured it is known. It writes the drafts and DreamKey, nothing else.
func (b *Book) Dream(ctx context.Context) error {
	now := b.now()
	var settled []Event
	var newest time.Time
	for _, e := range b.Friction(now.Add(-Window)) {
		if now.Sub(e.At) >= RetryWindow {
			settled = append(settled, e)
			if e.At.After(newest) {
				newest = e.At
			}
		}
	}
	if len(settled) == 0 {
		return nil
	}
	b.draftsMu.Lock()
	defer b.draftsMu.Unlock()
	if !b.dream.loaded {
		var mark dreamMark
		if b.h.Load != nil {
			if _, err := b.h.Load(ctx, DreamKey, &mark); err != nil {
				return err
			}
		}
		b.dream = dreamState{loaded: true, mark: mark}
	}
	if now.Sub(b.dream.mark.At) < DreamEvery || !newest.After(b.dream.mark.Through) {
		return nil
	}
	stored, err := b.loadDrafts(ctx)
	if err != nil {
		return err
	}
	if err := b.saveDrafts(ctx, DreamDrafts(stored, settled, now)); err != nil {
		return err
	}
	b.dream.mark = dreamMark{At: now.UTC(), Through: newest}
	if b.h.Save == nil {
		return nil
	}
	return b.h.Save(ctx, DreamKey, b.dream.mark)
}

type groupKey struct{ tool, code, skeleton string }

// DreamDrafts is the friction dream's step: the stored drafts merged with what events, the failed calls of the last
// Window, show at now.
//
// The failures are grouped by tool, code and skeleton, and a group that repeats (draftMin) is one draft, whose id comes
// from those three: it keeps its id and its review state from run to run.
//   - Its count, first and last failure are the group's in Window, and its evidence adds the group's conversations to
//     the ones it named before, newest first, at most MaxEvidence.
//   - While it is pending, its kind, runtime, retry and text follow the group. Once proposed, filed or dismissed they
//     stay as they were reviewed, and its state never changes here: a dismissed draft is not reopened, however often
//     its friction recurs, for the dismissal is a decision about that friction and its growing count stays visible.
//
// A stored draft whose group does not repeat in Window is left as it was, but a pending one whose last failure is older
// than Window goes: what it reported has stopped. New drafts follow the stored ones, the earliest friction first, and
// past MaxDrafts the drafts whose friction was seen longest ago go.
func DreamDrafts(stored []Draft, events []Event, now time.Time) []Draft {
	groups := map[groupKey][]Event{}
	for _, e := range events {
		k := groupKey{e.Tool, e.Code, e.Skeleton}
		groups[k] = append(groups[k], e)
	}
	out := slices.Clone(stored)
	at := map[groupKey]int{}
	for i, d := range out {
		k := groupKey{d.Tool, d.Code, d.Skeleton}
		if _, ok := at[k]; !ok {
			at[k] = i
		}
	}
	var made []Draft
	for k, g := range groups {
		if !repeats(g) {
			continue
		}
		if i, ok := at[k]; ok {
			out[i] = merged(out[i], g, now)
		} else {
			made = append(made, merged(Draft{ID: draftID(k), Status: DraftPending}, g, now))
		}
	}
	out = slices.DeleteFunc(out, func(d Draft) bool {
		return d.Status == DraftPending && d.LastAt.Before(now.Add(-Window))
	})
	slices.SortFunc(made, func(a, b Draft) int {
		return cmp.Or(a.FirstAt.Compare(b.FirstAt), strings.Compare(a.ID, b.ID))
	})
	out = append(out, made...)
	if extra := len(out) - MaxDrafts; extra > 0 {
		oldest := slices.Clone(out)
		slices.SortStableFunc(oldest, func(a, b Draft) int { return a.LastAt.Compare(b.LastAt) })
		gone := map[string]bool{}
		for _, d := range oldest[:extra] {
			gone[d.ID] = true
		}
		out = slices.DeleteFunc(out, func(d Draft) bool { return gone[d.ID] })
	}
	return out
}

func repeats(g []Event) bool {
	paired := slices.ContainsFunc(g, func(e Event) bool { return e.RetrySkeleton != "" })
	return len(g) >= draftMin || paired && len(g) >= draftMinPaired
}

// draftID is a group's draft id: the same group, the same id.
func draftID(k groupKey) string {
	sum := sha256.Sum256([]byte(k.tool + "\x00" + k.code + "\x00" + k.skeleton))
	return "fd_" + hex.EncodeToString(sum[:6])
}

// merged is d with what group g shows (DreamDrafts); UpdatedAt moves only when something else did.
func merged(d Draft, g []Event, now time.Time) Draft {
	next := d
	next.Tool, next.Code, next.Skeleton = g[0].Tool, g[0].Code, g[0].Skeleton
	next.Count = len(g)
	next.FirstAt, next.LastAt = g[0].At.UTC(), g[0].At.UTC()
	for _, e := range g[1:] {
		next.FirstAt, next.LastAt = minTime(next.FirstAt, e.At.UTC()), maxTime(next.LastAt, e.At.UTC())
	}
	next.Evidence = evidence(d.Evidence, g)
	if next.Status == DraftPending {
		next.Runtime = runtimeOf(g)
		next.RetrySkeleton = retryOf(g)
		next.Kind = DraftFix
		if next.RetrySkeleton != "" && !toolBroke(next.Code) {
			next.Kind = DraftNote
		}
		next.Text = draftText(next, conversations(g))
	}
	if !sameDraft(d, next) {
		next.UpdatedAt = now.UTC()
	}
	return next
}

// toolBroke: the code says the tool itself failed (Persona's error, an RPC method the node does not have), not that it
// refused how it was called. A retry that worked does not make that the caller's mistake: it is a fix, not a note.
func toolBroke(code string) bool {
	switch code {
	case "internal", "internal_error", "server_error", "panic", "unknown_method":
		return true
	}
	return false
}

// draftText is d's text from its template: a field note naming what fails and what worked, or a fix naming what fails,
// how often, in how many conversations and over which days. A skeleton too long for that is shortened (fitted).
func draftText(d Draft, conversations int) string {
	tool := textName(d.Tool)
	if d.Kind == DraftNote {
		return fitted(noteMax, d.Skeleton, d.RetrySkeleton, func(failed, worked string) string {
			return prompts.RenderToolFrictionNote(prompts.ToolFrictionNote{Tool: tool, Failed: failed, Code: d.Code,
				Worked: worked})
		})
	}
	from, to := d.FirstAt.UTC().Format(time.DateOnly), d.LastAt.UTC().Format(time.DateOnly)
	return fitted(fixMax, d.Skeleton, d.RetrySkeleton, func(failed, worked string) string {
		return prompts.RenderToolFrictionFix(prompts.ToolFrictionFix{Tool: tool, Failed: failed, Code: d.Code,
			Worked: worked, Count: d.Count, Conversations: conversations, From: from, To: to})
	})
}

// fitted renders a text with the skeletons failed and worked whole when it fits limit characters, else with what nests
// deepest in them shown as "…", one level more each time; the last resort is cutting the text.
func fitted(limit int, failed, worked string, render func(failed, worked string) string) string {
	var text string
	for depth := skeletonDepth + 1; depth >= 1; depth-- {
		text = render(abbreviate(failed, depth), abbreviate(worked, depth))
		if utf8.RuneCountInString(text) <= limit {
			return text
		}
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

// abbreviate is a skeleton with each object or array nested deeper than depth shown as "…": at depth 3,
// {steps:[{launch_app:{name:str}}]} is {steps:[{launch_app:…}]}.
func abbreviate(skeleton string, depth int) string {
	var b strings.Builder
	level, hidden := 0, 0 // hidden: the level of the object or array shown as "…", 0 while none is
	for _, r := range skeleton {
		switch r {
		case '{', '[':
			level++
			if hidden == 0 && level > depth {
				hidden = level
				b.WriteString("…")
			}
		case '}', ']':
			level--
			if hidden > level {
				if hidden == level+1 {
					hidden = 0 // the hidden one closes
				}
				continue
			}
		}
		if hidden == 0 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// textName is a tool's name as a draft's text names it: the characters a tool name has, at most 64. The name is the
// one a call asked for, which may be no tool's.
func textName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			if b.Len() == 64 {
				break
			}
		}
	}
	return b.String()
}

// evidence is the conversations of old and of g, each at its latest failure, newest first, at most MaxEvidence. A call
// that names no conversation is no evidence.
func evidence(old []Evidence, g []Event) []Evidence {
	latest := map[[2]string]time.Time{}
	note := func(machine, session string, at time.Time) {
		k := [2]string{machine, session}
		if session != "" && at.After(latest[k]) {
			latest[k] = at.UTC()
		}
	}
	for _, e := range old {
		note(e.Machine, e.Session, e.At)
	}
	for _, e := range g {
		note(e.Machine, e.Session, e.At)
	}
	list := make([]Evidence, 0, len(latest))
	for k, at := range latest {
		list = append(list, Evidence{Machine: k[0], Session: k[1], At: at})
	}
	slices.SortFunc(list, func(a, b Evidence) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(a.Machine, b.Machine), strings.Compare(a.Session, b.Session))
	})
	return list[:min(len(list), MaxEvidence)]
}

// conversations is how many conversations g's failures happened in; calls that name none count as one.
func conversations(g []Event) int {
	seen := map[[2]string]bool{}
	for _, e := range g {
		seen[[2]string{e.Machine, e.Session}] = true
	}
	return len(seen)
}

// runtimeOf is the runtime all of g's failures share; "" when they differ.
func runtimeOf(g []Event) string {
	for _, e := range g[1:] {
		if e.Runtime != g[0].Runtime {
			return ""
		}
	}
	return g[0].Runtime
}

// retryOf is the skeleton of the retry that cured most of g's failures, the latest one on a tie; "" when none was
// cured.
func retryOf(g []Event) string {
	count := map[string]int{}
	best := ""
	for _, e := range g { // oldest first: a later one wins a tie
		if e.RetrySkeleton == "" {
			continue
		}
		count[e.RetrySkeleton]++
		if count[e.RetrySkeleton] >= count[best] {
			best = e.RetrySkeleton
		}
	}
	return best
}

func sameDraft(a, b Draft) bool {
	return a.Kind == b.Kind && a.Runtime == b.Runtime && a.Count == b.Count && a.FirstAt.Equal(b.FirstAt) &&
		a.LastAt.Equal(b.LastAt) && a.RetrySkeleton == b.RetrySkeleton && a.Text == b.Text &&
		slices.EqualFunc(a.Evidence, b.Evidence, func(x, y Evidence) bool {
			return x.Machine == y.Machine && x.Session == y.Session && x.At.Equal(y.At)
		})
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
