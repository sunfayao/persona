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

	"github.com/sunfayao/persona/server/internal/approval"
	"github.com/sunfayao/persona/server/internal/errreport"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

const day = 24 * time.Hour

// input is a tool_note call's arguments (prompts/mcp/assistant/tool_note.md).
type input struct {
	Action     string `json:"action"`
	Tool       string `json:"tool"`
	Text       string `json:"text"`
	Machine    string `json:"machine"`
	Runtime    string `json:"runtime"`
	Evidence   string `json:"evidence"`
	ReviewDays int    `json:"review_days"`
	DraftID    string `json:"draft_id"`
	ID         string `json:"id"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
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
		return d.retireNote(ctx, in)
	case "dismiss":
		return d.dismiss(ctx, in)
	case "renew":
		return d.renew(ctx, c, in)
	case "file_fix":
		return d.fileFix(ctx, c, in)
	}
	return "", fmt.Errorf("action must be propose, list, retire, dismiss, renew or file_fix, not %q", in.Action)
}

// propose checks a note, keeps it as proposed and puts up its card: only the user's Allow approves it (approve); a
// card declined, unanswered or gone retires it (endProposal). With a draft_id, the draft gives the tool, the failure's
// code and, unless the call gives them, the text and the evidence.
func (d *Domain) propose(ctx context.Context, c Call, in input) (string, error) {
	n := Note{Tool: strings.TrimSpace(in.Tool), Runtime: strings.TrimSpace(in.Runtime), By: c.Session}
	text, evidence := in.Text, in.Evidence
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
		n.Tool, n.DraftID, n.Code = x.Tool, x.ID, x.Code
		text = cmp.Or(strings.TrimSpace(text), x.Text)
		evidence = cmp.Or(strings.TrimSpace(evidence), draftEvidence(x, c.Zone))
	}
	var err error
	if n.Text, err = noteText(text); err != nil {
		return "", err
	}
	if n.Evidence, err = noteEvidence(evidence); err != nil {
		return "", err
	}
	if n.Runtime != "" && n.Runtime != "claude" && n.Runtime != "codex" && n.Runtime != "agy" {
		return "", fmt.Errorf("runtime must be claude, codex or agy, not %q", n.Runtime)
	}
	days, err := reviewDays(in.ReviewDays)
	if err != nil {
		return "", err
	}
	if n.Tool == "" {
		return "", errors.New("tool is required: the tool the note is for")
	}
	tool, ok := d.tool(ctx, n.Tool)
	if !ok {
		return "", fmt.Errorf("no tool %q in the assistant's conversations: a note is for one of their tools, built-in or an extension's", n.Tool)
	}
	n.PromptDigest = Digest(tool)
	var where []string // the card's scope: the computer's name and the runtime, as they are written
	if m := strings.TrimSpace(in.Machine); m != "" {
		id, name, ok := d.h.Machine(ctx, m)
		if !ok {
			return "", fmt.Errorf("no computer %q: machines_list has the user's computers", m)
		}
		n.Machine, where = id, append(where, cmp.Or(name, id))
	}
	if n.Runtime != "" {
		where = append(where, n.Runtime)
	}
	if err := d.private(ctx, n); err != nil {
		return "", err
	}
	now := d.now()
	n.ID, n.CreatedAt, n.ReviewAt, n.Status = newID(), now, now.Add(time.Duration(days)*day), StatusProposed
	d.setLive(n.ID, true) // before it is stored: a Sweep meanwhile must not take it for a card that is gone
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		active := 0
		for _, o := range doc.Notes {
			if o.Status == StatusRetired {
				continue
			}
			if active++; o.Tool == n.Tool && o.Machine == n.Machine && o.Runtime == n.Runtime && strings.EqualFold(o.Text, n.Text) {
				return false, fmt.Errorf("already there: note %s (%s)", o.ID, o.Status)
			}
		}
		if active >= maxActive {
			return false, fmt.Errorf("%d notes are approved or proposed: retire some before proposing more", active)
		}
		doc.Notes = append(doc.Notes, n)
		return true, nil
	}); err != nil {
		d.setLive(n.ID, false)
		return "", err
	}
	summary := i18n.T(c.Lang, "assistant-tool_notes.card_propose", i18n.Args{"tool": n.Tool, "days": days, "text": n.Text})
	if len(where) > 0 {
		summary = i18n.T(c.Lang, "assistant-tool_notes.card_propose_scoped", i18n.Args{"tool": n.Tool,
			"where": strings.Join(where, ", "), "days": days, "text": n.Text})
	}
	shown, _ := json.Marshal(map[string]any{"tool": n.Tool, "text": n.Text, "where": strings.Join(where, ", "),
		"review_days": days, "evidence": n.Evidence})
	out, err := d.h.Confirm(ctx, Card{Machine: c.Machine, Session: c.Session, Summary: summary, Shown: shown,
		Key: "propose\x00" + n.Tool + "\x00" + n.Machine + "\x00" + n.Runtime + "\x00" + n.Text}, approval.Operation{
		Denied:     "The user declined the field note: it was not added.",
		Unanswered: "The user did not approve the field note: it was not added.",
		Apply:      func(ctx context.Context, _ proto.Resolution) (string, error) { return d.approve(ctx, n.ID, c.Zone) },
		Discard:    func(ctx context.Context, status string) { d.endProposal(ctx, n.ID, status) },
	})
	if err != nil {
		d.endProposal(ctx, n.ID, "unavailable")
		return "", err
	}
	return out.Text, nil
}

// approve is a proposal's Apply, on the user's Allow: the note goes into its tool's description for the review period
// it was proposed with, counted from now, and its draft is marked proposed. approval.ErrStale when the proposal no
// longer waits (it retired meanwhile).
func (d *Domain) approve(ctx context.Context, id string, zone *time.Location) (string, error) {
	defer d.setLive(id, false)
	now := d.now()
	var n Note
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		i := doc.find(id)
		if i < 0 || doc.Notes[i].Status != StatusProposed {
			return false, approval.ErrStale
		}
		p := &doc.Notes[i]
		p.Status, p.ApprovedAt, p.ReviewAt = StatusApproved, now, now.Add(p.ReviewAt.Sub(p.CreatedAt))
		n = *p
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
	return fmt.Sprintf("Approved: field note %s is at the end of %s's description in the assistant's conversations%s until %s, when it is due for review.",
		n.ID, n.Tool, d.scope(n), n.ReviewAt.In(zone).Format(time.DateOnly)), nil
}

// endProposal is a proposal's card ending without approving it (Discard, or a card that could not be shown): the note
// retires, saying how, unless it was approved.
func (d *Domain) endProposal(ctx context.Context, id, status string) {
	d.setLive(id, false)
	reason := fmt.Sprintf(reasonEnded, status)
	if status == "denied" {
		reason = reasonDeclined
	}
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		i := doc.find(id)
		if i < 0 || doc.Notes[i].Status != StatusProposed {
			return false, nil
		}
		retire(&doc.Notes[i], d.now(), reason)
		return true, nil
	}); err != nil {
		d.h.Logf("tool notes: end proposal %s: %v", id, err)
	}
}

// renew moves an approved note's review date to review_days from now, once the user approves on a card.
func (d *Domain) renew(ctx context.Context, c Call, in input) (string, error) {
	n, ok := d.note(ctx, strings.TrimSpace(in.ID))
	switch {
	case !ok:
		return "", fmt.Errorf("no field note %q: tool_note list shows them", in.ID)
	case n.Status != StatusApproved:
		return "", fmt.Errorf("note %s is %s: only an approved note is renewed; propose a retired one again", n.ID, n.Status)
	}
	days, err := reviewDays(in.ReviewDays)
	if err != nil {
		return "", err
	}
	if t, ok := d.tool(ctx, n.Tool); ok && Digest(t) != n.PromptDigest {
		return "", fmt.Errorf("%s's description changed since note %s was proposed, so the note retires; propose it again if it still holds", n.Tool, n.ID)
	}
	summary := i18n.T(c.Lang, "assistant-tool_notes.card_renew", i18n.Args{"tool": n.Tool, "days": days, "text": n.Text})
	shown, _ := json.Marshal(map[string]any{"note": n.ID, "tool": n.Tool, "text": n.Text, "review_days": days})
	out, err := d.h.Confirm(ctx, Card{Machine: c.Machine, Session: c.Session, Summary: summary, Shown: shown, Key: "renew\x00" + n.ID},
		approval.Operation{
			Denied:     fmt.Sprintf("The user declined: note %s keeps its review date.", n.ID),
			Unanswered: fmt.Sprintf("The user did not approve: note %s keeps its review date.", n.ID),
			Apply: func(ctx context.Context, _ proto.Resolution) (string, error) {
				return d.extend(ctx, n.ID, days, c.Zone)
			},
		})
	if err != nil {
		return "", err
	}
	return out.Text, nil
}

// extend is a renewal's Apply. approval.ErrStale when the note is no longer approved.
func (d *Domain) extend(ctx context.Context, id string, days int, zone *time.Location) (string, error) {
	now := d.now()
	var n Note
	if err := d.change(ctx, func(doc *doc) (bool, error) {
		i := doc.find(id)
		if i < 0 || doc.Notes[i].Status != StatusApproved {
			return false, approval.ErrStale
		}
		doc.Notes[i].ReviewAt = now.Add(time.Duration(days) * day)
		n = doc.Notes[i]
		return true, nil
	}); err != nil {
		return "", err
	}
	d.h.Changed(true)
	return fmt.Sprintf("Renewed: note %s is in %s's description until %s.", n.ID, n.Tool, n.ReviewAt.In(zone).Format(time.DateOnly)), nil
}

// retireNote takes a note out at once, with the reason given.
func (d *Domain) retireNote(ctx context.Context, in input) (string, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return "", errors.New("retire takes id, a note's id from tool_note list; a friction draft is set aside with dismiss")
	}
	reason := cmp.Or(clip(strings.Join(strings.Fields(in.Reason), " "), MaxText), "retired by the assistant")
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
		retire(&doc.Notes[i], now, reason)
		return true, nil
	}); err != nil {
		return "", err
	}
	d.h.Changed(was.shownAt(now))
	return fmt.Sprintf("Retired note %s: it is out of %s's description.", id, was.Tool), nil
}

// dismiss sets a pending friction draft aside.
func (d *Domain) dismiss(ctx context.Context, in input) (string, error) {
	id := strings.TrimSpace(in.DraftID)
	if id == "" {
		return "", errors.New("dismiss takes draft_id, a friction draft's id from tool_note list; a note is taken out with retire")
	}
	err := d.h.UpdateDraft(ctx, id, func(x *Draft) error {
		if x.Status != DraftPending {
			return fmt.Errorf("draft %s is %s, not pending", x.ID, x.Status)
		}
		x.Status = DraftDismissed
		return nil
	})
	if errors.Is(err, ErrNoDraft) {
		return "", fmt.Errorf("no friction draft %q: tool_note list shows them", id)
	}
	if err != nil {
		return "", err
	}
	d.h.Changed(false)
	return fmt.Sprintf("Dismissed draft %s.", id), nil
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
		return "", fmt.Errorf("draft %s is a %s draft, not a fix: propose it as a note (propose with its draft_id) or dismiss it", x.ID, x.Kind)
	case x.Status == DraftFiled:
		return fmt.Sprintf("Draft %s is filed already, as to-do %s.", x.ID, x.TodoID), nil
	case x.Status == DraftDismissed:
		return "", fmt.Errorf("draft %s was dismissed", x.ID)
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
			return fmt.Errorf("draft %s was dismissed meanwhile", y.ID)
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
	return fmt.Sprintf("%s %s “%s”%s. Draft %s is filed; once the to-do is done, the field notes for %s with %s retire as covered.",
		made, todo, title, bound, x.ID, x.Tool, x.Code), nil
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

// list answers tool_note list: the notes (retired ones only when asked for), those due for review, and the pending
// friction drafts.
func (d *Domain) list(ctx context.Context, c Call, in input) (string, error) {
	status := strings.TrimSpace(in.Status)
	switch status {
	case "", StatusProposed, StatusApproved, "due", StatusRetired:
	default:
		return "", fmt.Errorf("status must be proposed, approved, due or retired, not %q", in.Status)
	}
	tool, now := strings.TrimSpace(in.Tool), d.now()
	var notes, due []Note
	retired := 0
	for _, n := range d.held(ctx).Notes {
		if tool != "" && n.Tool != tool {
			continue
		}
		switch {
		case n.Status == StatusRetired:
			retired++
			if status == StatusRetired {
				notes = append(notes, n)
			}
		case n.Status == StatusApproved && !n.ReviewAt.After(now):
			if status == "" || status == "due" {
				due = append(due, n)
			}
		case status == "" || status == n.Status:
			notes = append(notes, n)
		}
	}
	slices.Reverse(notes) // newest first
	var b strings.Builder
	if len(notes) > 0 {
		b.WriteString("Field notes (an approved one is at the end of its tool's description until its review date; newest first):\n")
		for _, n := range notes {
			b.WriteString(d.noteLine(n, now, c.Zone) + "\n")
		}
	}
	if len(due) > 0 {
		b.WriteString("Due for review, out of the descriptions until renewed (renew the ones that still hold, retire the others):\n")
		for _, n := range due {
			b.WriteString(d.noteLine(n, now, c.Zone) + "\n")
		}
	}
	if status == "" {
		drafts, err := d.h.Drafts(ctx)
		if err != nil {
			fmt.Fprintf(&b, "The friction drafts cannot be read now: %v\n", err)
		}
		drafts = slices.DeleteFunc(drafts, func(x Draft) bool { return x.Status != DraftPending || tool != "" && x.Tool != tool })
		if len(drafts) > 0 {
			b.WriteString("Friction drafts waiting (propose a note draft by its draft_id, file_fix a fix draft, or dismiss one):\n")
			for _, x := range drafts {
				b.WriteString(draftLine(x, c.Zone) + "\n")
			}
		}
		if retired > 0 {
			fmt.Fprintf(&b, "%d retired notes: status \"retired\" lists them.\n", retired)
		}
	}
	if b.Len() == 0 {
		return "No field notes or friction drafts here.", nil
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func (d *Domain) noteLine(n Note, now time.Time, zone *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s %s", n.ID, n.Tool)
	var also []string
	if n.DraftID != "" {
		also = append(also, "from draft "+n.DraftID)
	}
	fmt.Fprintf(&b, "%s", d.scope(n))
	switch {
	case n.Status == StatusProposed:
		also = append(also, "waiting for the user's tap")
	case n.Status == StatusRetired:
		also = append(also, "retired "+n.RetiredAt.In(zone).Format(time.DateOnly)+": "+n.RetiredReason)
	case n.ReviewAt.After(now):
		also = append(also, "review on "+n.ReviewAt.In(zone).Format(time.DateOnly))
	default:
		also = append(also, "due for review since "+n.ReviewAt.In(zone).Format(time.DateOnly))
	}
	fmt.Fprintf(&b, ", %s: “%s”", strings.Join(also, ", "), n.Text)
	if n.Evidence != "" {
		fmt.Fprintf(&b, " Evidence: %s", n.Evidence)
	}
	return b.String()
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
	var b strings.Builder
	fmt.Fprintf(&b, "- %s %s draft for %s: %s %d times from %s to %s", x.ID, x.Kind, x.Tool, x.Code, x.Count,
		x.FirstAt.In(zone).Format(time.DateOnly), x.LastAt.In(zone).Format(time.DateOnly))
	if x.Runtime != "" {
		fmt.Fprintf(&b, " on %s", x.Runtime)
	}
	if x.Skeleton != "" {
		fmt.Fprintf(&b, ", arguments %s", x.Skeleton)
	}
	if x.RetrySkeleton != "" {
		fmt.Fprintf(&b, "; the retry that worked had %s", x.RetrySkeleton)
	}
	if x.Text != "" {
		fmt.Fprintf(&b, ": “%s”", x.Text)
	}
	return b.String()
}

// draftEvidence is a note's evidence when its draft gives it: how often and when the friction happened (the note
// keeps the draft's id beside it).
func draftEvidence(x Draft, zone *time.Location) string {
	s := fmt.Sprintf("%d failed calls with %s from %s to %s", x.Count, x.Code,
		x.FirstAt.In(zone).Format(time.DateOnly), x.LastAt.In(zone).Format(time.DateOnly))
	if x.RetrySkeleton != "" {
		s += "; a retry with other arguments worked"
	}
	return s
}

// private refuses a note whose text or evidence carries what the error reports' scrubber takes out (a secret, a path,
// an address, one of the user's computers' names): the note would be in every conversation's tool list.
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

func (d *Domain) note(ctx context.Context, id string) (Note, bool) {
	held := d.held(ctx)
	if i := held.find(id); i >= 0 && id != "" {
		return held.Notes[i], true
	}
	return Note{}, false
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

// noteEvidence is a note's evidence as kept: on one line, at most MaxEvidence characters.
func noteEvidence(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if n := utf8.RuneCountInString(s); n > MaxEvidence {
		return "", fmt.Errorf("evidence is %d characters; at most %d", n, MaxEvidence)
	}
	return s, nil
}

// reviewDays is review_days as kept: DefaultReviewDays when not given, at most MaxReviewDays.
func reviewDays(n int) (int, error) {
	switch {
	case n < 0:
		return 0, fmt.Errorf("review_days must be 1 to %d", MaxReviewDays)
	case n == 0:
		return DefaultReviewDays, nil
	}
	return min(n, MaxReviewDays), nil
}

// clip is s cut to at most n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
