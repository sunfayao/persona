package prompts

import (
	"slices"
	"strings"
)

// The supervision message of both audiences is one block per to-do: its signals merged into one line, its criteria
// numbered one per line with their state and evidence, and what its Agent said last (assistant/supervise_block). The
// blocks are built here from the entries; assistant/digestfmt decides which parts of a block the reader still needs.

// The audiences of a supervision message.
const (
	SuperviseForMain   = "main"   // a conversation (assistant/supervise)
	SuperviseForLeader = "leader" // a to-do's supervisor Agent (agents/worker_supervise)
)

// SuperviseCriterion is one done_when entry of a to-do with the latest report or check of it: State is pending, met or
// not_met; By says who recorded it (agent:<machine>/<session>, supervisor:<conversation>, judge, hub).
type SuperviseCriterion struct{ Text, State, Evidence, By string }

// SuperviseSignal is one signal of a block: what happened (What) and what comes with it (Detail, one line per fact).
// Show is false for a signal the reader has been told as it stands.
type SuperviseSignal struct {
	Kind, What, Detail string
	Own                bool
	Show               bool
}

// SuperviseCriterionLine is one criterion of a block, numbered from 1 in the to-do's order; Show is false for a line
// the reader has as it stands.
type SuperviseCriterionLine struct {
	N                         int
	Text, State, Evidence, By string
	Show                      bool
}

// SuperviseBlock is one to-do in a supervision message.
type SuperviseBlock struct {
	N               int
	Role            string
	ID, Title, Link string
	Own             bool // the supervisor Agent's own to-do, not a worker's
	Kinds           []string
	Multi           bool
	Signals         []SuperviseSignal
	Criteria        []SuperviseCriterionLine
	NoCriteria      bool // none were recorded
	ShowCriteria    bool // at least one line (or the lack of any) is told
	Partial         bool // only some criteria lines are told
	Last            string
	Session         string // the Agent whose words Last holds: where the rest is read when Last was cut
	Cut             bool
	Unchanged       bool   // the parts left out are as the reader has them (a later message about the same to-do)
	Gone            string // when set, the whole block is this one line
}

// SuperviseBlocks groups entries (in their order) into one block per to-do for audience role; fresh is a round that
// starts from a clean context (Supervise.Fresh). Every part is shown.
func SuperviseBlocks(entries []SuperviseEntry, role string, fresh bool) []SuperviseBlock {
	var out []SuperviseBlock
	at := map[string]int{}
	for _, e := range entries {
		i, seen := at[e.ID]
		if !seen || e.ID == "" {
			i = len(out)
			at[e.ID] = i
			out = append(out, SuperviseBlock{N: i + 1, Role: role, ID: e.ID, Title: e.Title, Link: e.Link, Own: e.Own,
				Criteria: criterionLines(e), NoCriteria: len(e.Criteria)+len(e.DoneWhen) == 0})
			out[i].ShowCriteria, out[i].Session = true, e.Session
		}
		b := &out[i]
		what, detail := "assistant/supervise_what", "assistant/supervise_detail"
		if role == SuperviseForLeader {
			what, detail = "agents/worker_supervise_what", "agents/worker_supervise_detail"
		}
		b.Signals = append(b.Signals, SuperviseSignal{Kind: e.Kind, Own: e.Own, Show: true,
			What: strings.TrimSpace(render(what, e)),
			Detail: strings.TrimSpace(render(detail, struct {
				E     SuperviseEntry
				Fresh bool
			}{e, fresh}))})
		if !slices.Contains(b.Kinds, e.Kind) {
			b.Kinds = append(b.Kinds, e.Kind)
		}
		if b.Last == "" && e.Last != "" {
			b.Last, b.Session = e.Last, e.Session
		}
		b.Multi = len(b.Signals) > 1
		b.Link = cmpOr(b.Link, e.Link)
	}
	return out
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// criterionLines are e's criteria as lines: the ledger's entries, else its done_when as pending ones.
func criterionLines(e SuperviseEntry) []SuperviseCriterionLine {
	list := e.Criteria
	if len(list) == 0 {
		for _, text := range e.DoneWhen {
			list = append(list, SuperviseCriterion{Text: text, State: "pending"})
		}
	}
	out := make([]SuperviseCriterionLine, len(list))
	for i, c := range list {
		out[i] = SuperviseCriterionLine{N: i + 1, Text: c.Text, State: criterionState(c.State), Show: true,
			Evidence: strings.Join(strings.Fields(c.Evidence), " "), By: criterionBy(c.By)}
	}
	return out
}

func criterionState(s string) string {
	switch s {
	case "met":
		return "met"
	case "not_met":
		return "not met"
	}
	return "pending"
}

// criterionBy names who recorded a criterion's reading without the ids.
func criterionBy(by string) string {
	switch kind, _, _ := strings.Cut(by, ":"); kind {
	case "agent":
		return "the Agent"
	case "supervisor":
		return "the supervisor"
	default:
		return by // hub, judge
	}
}

// RenderSuperviseBlock is one block as the message prints it; digestfmt measures it to keep a block in its size.
func RenderSuperviseBlock(b SuperviseBlock) string { return render("assistant/supervise_block", b) }

// RenderDigestWhole is the one-off context block that tells a conversation the blocks of a supervision message whole,
// when it was compacted after the message was prepared with only what changed.
func RenderDigestWhole(blocks []SuperviseBlock) string {
	return render("assistant/digest_whole", struct{ Blocks []SuperviseBlock }{blocks})
}

// RenderSupervisionPolicyNotice is the full supervision policy as a one-off context block, for a conversation whose
// standing rules carry only a short supervision section (assistant/supervisionpolicy).
func RenderSupervisionPolicyNotice() string { return render("assistant/policy_notice", nil) }
