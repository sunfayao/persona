package toolnotes

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/errreport"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

const day = 24 * time.Hour

// input is a tool_note call's arguments (prompts/mcp/assistant/tool_note.md).
type input struct {
	Action  string `json:"action"`
	Tool    string `json:"tool"`
	Text    string `json:"text"`
	Machine string `json:"machine"`
	Runtime string `json:"runtime"`
	DraftID string `json:"draft_id"`
	ID      string `json:"id"`
}

// errNotPending: a draft has left pending since it was read.
var errNotPending = errors.New("the draft is not pending any more")

// Tool runs one tool_note call of conversation c; its answer and its errors are for the model.
func (d *Domain) Tool(ctx context.Context, c Call, raw json.RawMessage) (string, error) {
	var in input
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", fmt.Errorf("tool_note arguments: %v", err)
	}
	if c.Zone == nil {
		c.Zone = time.UTC
	}
	switch in.Action {
	case "propose":
		return d.propose(ctx, c, in)
	case "list":
		return d.list(ctx, c, in)
	case "retire":
		return d.retire(ctx, in)
	case "file_fix":
		return d.fileFix(ctx, c, in)
	}
	return "", fmt.Errorf("action must be propose, list, retire or file_fix, not %q", in.Action)
}

// propose checks a note and adds it directly as approved so it immediately ends its tool's description for ReviewDays
// (replacing any older duplicate due for review). With a draft_id, the draft gives the tool, the failure's code, the
// evidence and, unless the call gives one, the text.
func (d *Domain) propose(ctx context.Context, c Call, in input) (string, error) {
	n := Note{Tool: strings.TrimSpace(in.Tool), Runtime: strings.TrimSpace(in.Runtime), By: c.Session}
	text := in.Text
	if id := strings.TrimSpace(in.DraftID); id != "" {
		x, err := d.draft(ctx, id)
		if err != nil {
			return "", err
		}
		switch {
		case x.Status != DraftPending:
			return "", fmt.Errorf("draft %s is %s, not pending: tool_note list shows the pending ones", x.ID, x.Status)
		case n.Tool != "" && n.Tool != x.Tool:
			return "", fmt.Errorf("draft %s is about %s, not %s", x.ID, x.Tool, n.Tool)
		}
		n.Tool, n.DraftID, n.Code, n.Evidence = x.Tool, x.ID, x.Code, draftEvidence(x, c.Zone)
		text = cmp.Or(strings.TrimSpace(text), x.Text)
	}
	var err error
	if n.Text, err = noteText(text); err != nil {
		return "", err
	}
	if n.Runtime != "" && n.Runtime != "claude" && n.Runtime != "codex" && n.Runtime != "agy" {
		return "", fmt.Errorf("runtime must be claude, codex or agy, not %q", n.Runtime)
	}
	if n.Tool == "" {
		return "", errors.New("tool is required: the tool the note is for")
	}
	tool, ok := d.tool(ctx, n.Tool)
	if !ok {
		return "", fmt.Errorf("no tool %q in your tool list: a note is for one of its tools", n.Tool)
	}
	n.PromptDigest = Digest(tool)
	if m := strings.TrimSpace(in.Machine); m != "" {
		id, _, ok := d.h.Machine(ctx, m)
		if !ok {
			return "", fmt.Errorf("no computer %q: machines_list has the user's computers", m)
		}
		n.Machine = id
	}
	if err := d.private(ctx, n); err != nil {
		return "", err
	}
	now := d.now()
	n.ID, n.CreatedAt, n.ApprovedAt, n.ReviewAt, n.Status = newID(), now, now, now.Add(ReviewDays*day), StatusApproved
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		active := 0
		for _, o := range doc.Notes {
			if o.Status == StatusRetired || o.Status == StatusApproved && !o.ReviewAt.After(now) {
				continue // a note due for review is replaced below
			}
			if active++; o.sameAs(n) {
				return false, fmt.Errorf("already there: note %s (%s)", o.ID, o.Status)
			}
		}
		if active >= maxActive {
			return false, fmt.Errorf("%d notes are approved or proposed: retire some first", active)
		}
		for j := range doc.Notes {
			if o := &doc.Notes[j]; o.Status == StatusApproved && o.sameAs(n) {
				retire(o, now, "proposed again as "+n.ID)
			}
		}
		doc.Notes = append(doc.Notes, n)
		return true, nil
	}); err != nil {
		return "", err
	}
	if n.DraftID != "" {
		if err := d.h.UpdateDraft(ctx, n.DraftID, func(x *Draft) error {
			if x.Status != DraftPending {
				return errNotPending
			}
			x.Status, x.NoteID = DraftProposed, n.ID
			return nil
		}); err != nil && !errors.Is(err, errNotPending) {
			d.h.Logf("tool notes: mark draft %s proposed: %v", n.DraftID, err)
		}
	}
	d.h.Changed(true)
	return fmt.Sprintf("Approved: note %s now ends %s's description%s, until %s: “%s”", n.ID, n.Tool, d.scope(n),
		n.ReviewAt.In(c.Zone).Format(time.DateOnly), n.Text), nil
}

// sameAs: o and n are the same note, in the same place.
func (o Note) sameAs(n Note) bool {
	return o.Tool == n.Tool && o.Machine == n.Machine && o.Runtime == n.Runtime && strings.EqualFold(o.Text, n.Text)
}

// retire takes a note out at once (id), or sets a pending friction draft aside (draft_id).
func (d *Domain) retire(ctx context.Context, in input) (string, error) {
	id, draft := strings.TrimSpace(in.ID), strings.TrimSpace(in.DraftID)
	switch {
	case (id == "") == (draft == ""):
		return "", errors.New("retire takes id, a note's, or draft_id, a friction draft's (tool_note list has both)")
	case draft != "":
		err := d.h.UpdateDraft(ctx, draft, func(x *Draft) error {
			if x.Status != DraftPending {
				return fmt.Errorf("draft %s is %s, not pending", x.ID, x.Status)
			}
			x.Status = DraftDismissed
			return nil
		})
		if errors.Is(err, ErrNoDraft) {
			return "", fmt.Errorf("no friction draft %q: tool_note list shows them", draft)
		}
		if err != nil {
			return "", err
		}
		d.h.Changed(false)
		return fmt.Sprintf("Set draft %s aside.", draft), nil
	}
	now := d.now()
	var was Note
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		i := doc.find(id)
		switch {
		case i < 0:
			return false, fmt.Errorf("no field note %q: tool_note list shows them", id)
		case doc.Notes[i].Status == StatusRetired:
			return false, fmt.Errorf("note %s is retired already", id)
		}
		was = doc.Notes[i]
		retire(&doc.Notes[i], now, "retired on request")
		return true, nil
	}); err != nil {
		return "", err
	}
	d.h.Changed(was.shownAt(now))
	return fmt.Sprintf("Retired note %s: it is out of %s's description.", id, was.Tool), nil
}

// fileFix makes a fix draft (a defect of Persona's own) a to-do, the way todo_create makes one, bound to Persona's
// repository when it is saved in Persona: "Fix <tool>: <code>", with the draft's text, how often and when it failed,
// the arguments' skeletons and links to the conversations. The draft is marked filed with the to-do, and the fix is
// kept so that the to-do, once done, retires the notes of its tool and code (Sweep).
func (d *Domain) fileFix(ctx context.Context, c Call, in input) (string, error) {
	id := strings.TrimSpace(in.DraftID)
	if id == "" {
		return "", errors.New("file_fix takes draft_id, a fix draft's id from tool_note list")
	}
	x, err := d.draft(ctx, id)
	if err != nil {
		return "", err
	}
	switch {
	case x.Kind != DraftFix:
		return "", fmt.Errorf("draft %s is a %s draft, not a fix: propose it as a note (propose with its draft_id) or retire it", x.ID, x.Kind)
	case x.Status == DraftFiled:
		return fmt.Sprintf("Draft %s is filed already, as to-do %s.", x.ID, x.TodoID), nil
	case x.Status == DraftDismissed:
		return "", fmt.Errorf("draft %s was set aside", x.ID)
	}
	title := i18n.T(c.Lang, "assistant-tool_notes.fix_title", i18n.Args{"tool": x.Tool, "code": x.Code})
	repo, saved := d.h.Repository(ctx, PersonaRepository)
	var bind *Repository
	if saved {
		bind = &repo
	}
	todo, existed, err := d.h.FileTodo(ctx, c, title, d.fixNotes(ctx, c, x, saved), bind)
	if err != nil {
		return "", fmt.Errorf("the to-do could not be made, nothing changed: %w", err)
	}
	if err := d.h.UpdateDraft(ctx, x.ID, func(y *Draft) error {
		if y.Status == DraftDismissed {
			return fmt.Errorf("draft %s was set aside meanwhile", y.ID)
		}
		y.Status, y.TodoID = DraftFiled, todo
		return nil
	}); err != nil {
		return "", fmt.Errorf("to-do %s is there, but draft %s could not be marked filed (%v): file_fix it again", todo, x.ID, err)
	}
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		if slices.ContainsFunc(doc.Fixes, func(f Fix) bool { return f.TodoID == todo && f.Tool == x.Tool && f.Code == x.Code }) {
			return false, nil
		}
		doc.Fixes = append(doc.Fixes, Fix{DraftID: x.ID, TodoID: todo, Tool: x.Tool, Code: x.Code, FiledAt: d.now()})
		return true, nil
	}); err != nil {
		d.h.Logf("tool notes: keep fix %s: %v", todo, err)
	}
	d.h.Changed(false)
	made := "Filed as to-do"
	if existed {
		made = "The open to-do of the same title takes it:"
	}
	bound := " (" + PersonaRepository + " is not saved in Persona: the to-do names it)"
	if saved {
		bound = ", bound to " + PersonaRepository
	}
	return fmt.Sprintf("%s %s “%s”%s. Once it is done, the notes on %s for %s retire.", made, todo, title, bound, x.Tool, x.Code), nil
}

// fixNotes is a fix's to-do body, in the user's language.
func (d *Domain) fixNotes(ctx context.Context, c Call, x Draft, saved bool) string {
	var lines []string
	if t := strings.TrimSpace(x.Text); t != "" {
		lines = append(lines, t, "")
	}
	lines = append(lines, i18n.T(c.Lang, "assistant-tool_notes.fix_seen", i18n.Args{"count": x.Count,
		"first": x.FirstAt.In(c.Zone).Format(time.DateOnly), "last": x.LastAt.In(c.Zone).Format(time.DateOnly)}))
	if x.Skeleton != "" {
		lines = append(lines, i18n.T(c.Lang, "assistant-tool_notes.fix_skeleton", i18n.Args{"skeleton": x.Skeleton}))
	}
	if x.RetrySkeleton != "" {
		lines = append(lines, i18n.T(c.Lang, "assistant-tool_notes.fix_retry", i18n.Args{"skeleton": x.RetrySkeleton}))
	}
	if links := slices.DeleteFunc(d.h.Links(ctx, x.Evidence), func(l string) bool { return l == "" }); len(links) > 0 {
		lines = append(lines, i18n.T(c.Lang, "assistant-tool_notes.fix_conversations", i18n.Args{"links": strings.Join(links, " ")}))
	}
	if !saved {
		lines = append(lines, i18n.T(c.Lang, "assistant-tool_notes.fix_repository", i18n.Args{"repository": PersonaRepository}))
	}
	return strings.Join(lines, "\n")
}

// list answers tool_note list: the notes that are not retired (those due for review apart) and the pending friction
// drafts, of one tool when the call names it.
func (d *Domain) list(ctx context.Context, c Call, in input) (string, error) {
	tool, now := strings.TrimSpace(in.Tool), d.now()
	var notes, due []Note
	for _, n := range d.held(ctx).Notes {
		switch {
		case n.Status == StatusRetired || tool != "" && n.Tool != tool:
		case n.Status == StatusApproved && !n.ReviewAt.After(now):
			due = append(due, n)
		default:
			notes = append(notes, n)
		}
	}
	drafts, err := d.h.Drafts(ctx)
	if err != nil {
		return "", fmt.Errorf("the friction drafts cannot be read now: %w", err)
	}
	drafts = slices.DeleteFunc(drafts, func(x Draft) bool { return x.Status != DraftPending || tool != "" && x.Tool != tool })
	var b strings.Builder
	section := func(head string, lines []string) {
		if len(lines) > 0 {
			b.WriteString(head + "\n" + strings.Join(lines, "\n") + "\n")
		}
	}
	section("Field notes:", d.noteLines(notes, now, c.Zone))
	section("Due for review, out of the descriptions (propose again what still holds, retire the rest):", d.noteLines(due, now, c.Zone))
	var lines []string
	for _, x := range drafts {
		lines = append(lines, draftLine(x, c.Zone))
	}
	section("Friction drafts (propose a note draft by draft_id, file_fix a fix draft, or retire one):", lines)
	if b.Len() == 0 {
		return "No field notes or friction drafts.", nil
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func (d *Domain) noteLines(notes []Note, now time.Time, zone *time.Location) []string {
	var out []string
	for _, n := range notes {
		state := "waiting for the user's tap"
		switch {
		case n.Status != StatusApproved:
		case n.ReviewAt.After(now):
			state = "until " + n.ReviewAt.In(zone).Format(time.DateOnly)
		default:
			state = "due since " + n.ReviewAt.In(zone).Format(time.DateOnly)
		}
		line := fmt.Sprintf("- %s %s%s, %s: “%s”", n.ID, n.Tool, d.scope(n), state, n.Text)
		if n.DraftID != "" {
			line += " (draft " + n.DraftID + ": " + n.Evidence + ")"
		}
		out = append(out, line)
	}
	return out
}

// scope is where a note holds, for the model: " (on <computer>, agy only)", "" for everywhere.
func (d *Domain) scope(n Note) string {
	var parts []string
	if n.Machine != "" {
		parts = append(parts, "on "+cmp.Or(d.h.MachineName(n.Machine), n.Machine))
	}
	if n.Runtime != "" {
		parts = append(parts, n.Runtime+" only")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func draftLine(x Draft, zone *time.Location) string {
	line := fmt.Sprintf("- %s %s draft for %s: %s %d times from %s to %s", x.ID, x.Kind, x.Tool, x.Code, x.Count,
		x.FirstAt.In(zone).Format(time.DateOnly), x.LastAt.In(zone).Format(time.DateOnly))
	if x.Runtime != "" {
		line += " on " + x.Runtime
	}
	if x.Skeleton != "" {
		line += ", arguments " + x.Skeleton
	}
	if x.RetrySkeleton != "" {
		line += "; the retry that worked had " + x.RetrySkeleton
	}
	if x.Text != "" {
		line += ": “" + x.Text + "”"
	}
	return line
}

// draftEvidence is a note's evidence when its draft gives it: how often and when the friction happened.
func draftEvidence(x Draft, zone *time.Location) string {
	s := fmt.Sprintf("%d failed calls with %s from %s to %s", x.Count, x.Code,
		x.FirstAt.In(zone).Format(time.DateOnly), x.LastAt.In(zone).Format(time.DateOnly))
	if x.RetrySkeleton != "" {
		s += "; a retry with other arguments worked"
	}
	return s
}

// private refuses a note whose text or evidence carries what the error reports' scrubber takes out (a secret, a path,
// an address, one of the user's computers' names): the note would be in every tool list.
func (d *Domain) private(ctx context.Context, n Note) error {
	sc := errreport.NewScrubber(d.h.Private(ctx)...)
	for _, f := range []struct{ name, v string }{{"text", n.Text}, {"evidence", n.Evidence}} {
		if got := sc.Text(f.v); got != f.v {
			return fmt.Errorf("not proposed: the %s carries what a field note must not (a secret, a path, an address or a computer's name); scrubbed it reads “%s”. Write it without them: machine says which computer a note holds on", f.name, got)
		}
	}
	return nil
}

func (d *Domain) draft(ctx context.Context, id string) (Draft, error) {
	list, err := d.h.Drafts(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("the friction drafts cannot be read now: %w", err)
	}
	if i := slices.IndexFunc(list, func(x Draft) bool { return x.ID == id }); i >= 0 {
		return list[i], nil
	}
	return Draft{}, fmt.Errorf("no friction draft %q: tool_note list shows them", id)
}

func (d *Domain) tool(ctx context.Context, name string) (proto.Tool, bool) {
	tools := d.h.Tools(ctx)
	if i := slices.IndexFunc(tools, func(t proto.Tool) bool { return t.Name == name }); i >= 0 {
		return tools[i], true
	}
	return proto.Tool{}, false
}

// noteText is a note's text as kept: trimmed, one line of at most MaxText characters; an error says what to change.
func noteText(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case s == "":
		return "", errors.New("text is required: one line saying what works with the tool")
	case strings.ContainsAny(s, "\r\n"):
		return "", errors.New("text must be one line")
	case n > MaxText:
		return "", fmt.Errorf("text is %d characters; a note is at most %d", n, MaxText)
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		return "", errors.New("text holds a control character")
	}
	return s, nil
}
