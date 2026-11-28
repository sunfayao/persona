// Package taskleader is the rule book of a to-do's supervisor Agent: the task leader a to-do gets from task_plan with
// role "leader". One supervisor runs one to-do with its workers, each a to-do of its own beneath it, and stands
// between them and the conversation that handed the to-do out (the main conversation), so that conversation hears
// about the to-do only twice: when the supervisor has checked the whole of it (a completion) and when something needs
// the owner or that conversation to decide (an alignment). Everything else the supervisor handles itself, or the hub
// does without a model.
//
// The package decides where each supervision signal goes once a to-do has a supervisor (Route), when the supervisor
// may report its to-do done (CompletionRefusal), and what the supervisor's scoped tools take (DecodeRead, DecodeSend,
// DecodeScope). It is pure: no IO and no clock. The assistant root reads the to-dos and acts on what it decides.
package taskleader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Destination is where a signal goes.
type Destination string

const (
	// ToSupervisor: into the supervisor's own session, in its next supervision message. Delivering it resumes a
	// supervisor that was closed, and when it cannot be reached at all the hub escalates (an alignment).
	ToSupervisor Destination = "supervisor"
	// ToMain: to the conversation that supervises the root: a completion or an alignment.
	ToMain Destination = "main"
	// AsBefore: the route the hub takes without a supervisor (the owner's cards, Inbox news).
	AsBefore Destination = "as_before"
	// Absorbed: no model is woken. The to-do's page still shows what happened.
	Absorbed Destination = "absorbed"
)

// ReportCompletion is the Report of the review signal the hub raises when the supervisor reports its to-do done and
// the hub's checks pass (CompletionRefusal): the one signal of the root that is a completion.
const ReportCompletion = "completion"

// The kinds of supervision signals, as the assistant names them (its sig* constants), and the kinds of reports.
const (
	KindReview                = "review"
	KindOwnerChoice           = "owner_choice"
	KindIdle                  = "idle"
	KindUnjudged              = "unjudged"
	KindNeedsYou              = "needs_you"
	KindTimedOut              = "timed_out"
	KindStalled               = "stalled"
	KindNoProgress            = "no_progress"
	KindAllStopped            = "all_stopped"
	KindInterrupted           = "interrupted"
	KindFailed                = "failed"
	KindStarted               = "started"
	KindPRConflict            = "pr_conflict"
	KindDraftPR               = "draft_pr"
	KindPREvent               = "pr_event"
	KindReport                = "report"
	KindEscalated             = "escalated"
	KindOrphaned              = "orphaned"
	KindAsk                   = "ask"
	KindDecide                = "decide"
	KindDecisionReply         = "decision_reply"
	KindCheck                 = "check"
	KindTodoReady             = "todo_ready"
	KindPRMerged              = "pr_merged"
	KindParentCloseout        = "parent_closeout"
	KindRecheck               = "recheck"
	KindUndecidable           = "undecidable"
	KindOwnerCannotAnswer     = "owner_cannot_answer"
	KindParkedDue             = "parked_due"
	KindParkDecided           = "park_decided"
	KindParkedSummary         = "parked_summary"
	KindNeedsUserExternalInfo = "needs_user_external_info"
	KindInbox                 = "inbox"
	KindDeliveryHold          = "delivery_hold"
	KindCardlessWait          = "cardless_wait"
	KindOverdue               = "overdue"
	KindEstimateDue           = "estimate_due"
	KindHandoffResult         = "handoff_result"

	ReportProgress = "progress"
	ReportQuestion = "question"
	ReportBlocked  = "blocked"
	ReportDone     = "done"
	// CheckDecided is a check signal's report when the owner answered the decision a linked check raised.
	CheckDecided = "decided"
)

// Kinds are every supervision signal kind, in the order of Route's table.
var Kinds = []string{
	KindReview, KindOwnerChoice, KindIdle, KindUnjudged, KindNeedsYou, KindTimedOut, KindStalled, KindNoProgress,
	KindAllStopped, KindInterrupted, KindFailed, KindStarted, KindPRConflict, KindDraftPR, KindPREvent, KindReport,
	KindEscalated, KindOrphaned, KindAsk, KindDecide, KindDecisionReply, KindCheck, KindTodoReady, KindPRMerged,
	KindParentCloseout, KindRecheck, KindUndecidable, KindOwnerCannotAnswer, KindParkedDue, KindParkDecided,
	KindParkedSummary, KindNeedsUserExternalInfo, KindInbox, KindDeliveryHold, KindCardlessWait, KindOverdue,
	KindEstimateDue, KindHandoffResult,
}

// ownerDecisions are the kinds about a decision the owner holds or answered: the conversation that talks with the
// owner keeps them.
var ownerDecisions = map[string]bool{
	KindOwnerChoice: true, KindDecisionReply: true, KindRecheck: true, KindUndecidable: true, KindOwnerCannotAnswer: true,
	KindParkDecided: true, KindNeedsUserExternalInfo: true, KindDeliveryHold: true, KindCardlessWait: true,
	KindHandoffResult: true,
}

// Route is where signal kind (with report, a report's or a check's kind) about the supervisor's own to-do goes. The
// main conversation gets its completion (a review whose report is ReportCompletion), the supervisor's own questions
// and blocks, and the decisions only it and the owner take: nothing else. The workers' signals are not routed here:
// every one of them goes to the supervisor (the assistant's leaderRoute), but for the decisions it sends up.
func Route(kind, report string) Destination {
	switch kind {
	case KindAsk, KindInbox:
		return AsBefore // the owner's cards, and news the Inbox already shows the owner
	case KindEscalated, KindDecide:
		return ToMain // what the supervisor or a worker of it needs decided above it
	}
	if ownerDecisions[kind] {
		return ToMain
	}
	switch kind {
	case KindReview:
		if report == ReportCompletion {
			return ToMain // the completion: the supervisor checked the whole to-do
		}
		return ToSupervisor // any other wait for review is the supervisor's to finish with its completion
	case KindReport:
		switch report {
		case ReportQuestion, ReportBlocked:
			return ToMain // the supervisor's own question or block: an alignment
		}
		return Absorbed // progress wakes nobody; done becomes the completion's review, never a second entry
	case KindFailed:
		return ToMain // the supervisor did not start: its dispatcher remedies its own start
	case KindIdle, KindUnjudged, KindStalled, KindInterrupted, KindStarted, KindParkedSummary:
		return Absorbed // a supervisor that pauses waits for its workers; the hub continues a cut-off turn itself
	case KindCheck:
		if report == CheckDecided {
			return ToMain
		}
		return ToSupervisor
	}
	// timed_out, orphaned, no_progress, all_stopped, needs_you, the PR signals, todo_ready, parent_closeout, the
	// estimates and parked_due: the supervisor acts on them; delivering them wakes or resumes it.
	return ToSupervisor
}

// Criterion is one done_when of the supervisor's own to-do as its ledger has it now.
type Criterion struct {
	Text     string
	State    string // met, not_met or pending ("" pending)
	Evidence string
}

// WorkerState is one worker to-do of the supervisor as the hub has it now.
type WorkerState struct {
	ID, Title string
	Open      bool
}

// CompletionRefusal says why the supervisor may not report its to-do done yet, "" when it may: every worker is
// closed, and every done_when of its own to-do is met with evidence.
func CompletionRefusal(criteria []Criterion, workers []WorkerState) string {
	var open []string
	for _, w := range workers {
		if w.Open {
			open = append(open, fmt.Sprintf("%s (%q)", w.ID, w.Title))
		}
	}
	var unmet []string
	for i, c := range criteria {
		if c.State != "met" || strings.TrimSpace(c.Evidence) == "" {
			unmet = append(unmet, fmt.Sprintf("%d %q", i+1, c.Text))
		}
	}
	var why []string
	if len(open) > 0 {
		why = append(why, fmt.Sprintf("%d worker(s) are still open: %s. Close each with task_review first (accepted or cancelled)",
			len(open), strings.Join(open, ", ")))
	}
	if len(unmet) > 0 {
		why = append(why, fmt.Sprintf("criteria not met with evidence: %s. Report each as met with its evidence in criteria "+
			"(number, state met, evidence)", strings.Join(unmet, "; ")))
	}
	return strings.Join(why, ". ")
}

// ReadArgs is the supervisor's agent_read: one of its workers and how many recent exchanges.
type ReadArgs struct {
	TodoID string `json:"todo_id"`
	Turns  int    `json:"turns"`
}

// SendArgs is the supervisor's agent_send: one of its workers and the message.
type SendArgs struct {
	TodoID string `json:"todo_id"`
	Text   string `json:"text"`
}

// ScopeArgs is the supervisor's task_scope: a worker's done_when or expect_minutes, or its own to-do's
// expect_minutes, with what changes and why.
type ScopeArgs struct {
	TodoID        string    `json:"todo_id"`
	Change        string    `json:"change"`
	Why           string    `json:"why"`
	DoneWhen      *[]string `json:"done_when"`
	Add           []string  `json:"add"`
	Drop          []int     `json:"drop"`
	Edit          []Edit    `json:"edit"`
	ExpectMinutes int       `json:"expect_minutes"`
}

// Edit is one criterion's new text, named by its one-based number in done_when.
type Edit struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// SendMax bounds a message the supervisor sends a worker.
const SendMax = 4000

// DecodeRead reads the supervisor's agent_read arguments; only the fields it lists are taken.
func DecodeRead(raw json.RawMessage) (ReadArgs, error) {
	var a ReadArgs
	if err := strict(raw, &a); err != nil {
		return a, fmt.Errorf("agent_read takes todo_id and turns only: a worker of yours, by its to-do (%v)", err)
	}
	a.TodoID = strings.TrimSpace(a.TodoID)
	if a.TodoID == "" {
		return a, errors.New("todo_id is required: the worker's to-do, as its supervision message names it")
	}
	if a.Turns < 0 || a.Turns > 10 {
		return a, errors.New("turns is from 1 to 10 (0: the last 2)")
	}
	return a, nil
}

// DecodeSend reads the supervisor's agent_send arguments; only the fields it lists are taken.
func DecodeSend(raw json.RawMessage) (SendArgs, error) {
	var a SendArgs
	if err := strict(raw, &a); err != nil {
		return a, fmt.Errorf("agent_send takes todo_id and text only: a message to a worker of yours; new work is a "+
			"new worker (task_plan) (%v)", err)
	}
	a.TodoID, a.Text = strings.TrimSpace(a.TodoID), strings.TrimSpace(a.Text)
	switch {
	case a.TodoID == "":
		return a, errors.New("todo_id is required: the worker's to-do, as its supervision message names it")
	case a.Text == "":
		return a, errors.New("text is empty")
	case len(a.Text) > SendMax:
		return a, fmt.Errorf("text is longer than %d bytes: say it shorter, or put the detail in a file the worker can read", SendMax)
	}
	return a, nil
}

// DecodeScope reads the supervisor's task_scope arguments; only the fields it lists are taken.
func DecodeScope(raw json.RawMessage) (ScopeArgs, error) {
	var a ScopeArgs
	if err := strict(raw, &a); err != nil {
		return a, fmt.Errorf("task_scope takes todo_id, change, why, done_when, add, drop, edit and expect_minutes only (%v)", err)
	}
	a.TodoID, a.Change, a.Why = strings.TrimSpace(a.TodoID), strings.TrimSpace(a.Change), strings.TrimSpace(a.Why)
	switch {
	case a.TodoID == "":
		return a, errors.New("todo_id is required")
	case a.Change == "" || a.Why == "":
		return a, errors.New("task_scope needs change (what changes, in a sentence the owner can read) and why")
	case a.ExpectMinutes < 0:
		return a, errors.New("expect_minutes is zero or a positive number of minutes")
	}
	return a, nil
}

// strict decodes one JSON object into v, refusing unknown fields and anything after it.
func strict(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("one JSON object only")
	}
	return nil
}
