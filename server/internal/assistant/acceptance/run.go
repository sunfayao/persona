package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/textfmt"
)

// States of a check's result.
const (
	Met    = "met"
	NotMet = "not_met"
	// Cannot: the channel could not answer (no GitHub access, no pull request on record, a computer offline). It is
	// never counted as met and never skipped.
	Cannot = "cannot"
)

// CI states a channel answers with.
const (
	CISuccess = "success"
	CIFailure = "failure"
	CIPending = "pending"
	CINone    = "none"
)

// Page is what an HTTP GET answered: the status and at most MostBody bytes of the body.
type Page struct {
	Status int
	Body   []byte
}

// Channels are the ways a check reads the world; the root implements them and tests fake them. An error from one
// means it could not answer: the check's result is Cannot, with the error as its reason.
type Channels interface {
	// PullRequests are the pull request URLs on record for the to-do.
	PullRequests(ctx context.Context) []string
	Merged(ctx context.Context, pr string) (bool, error)
	// Deployed says whether the running deployment contains the pull request's change.
	Deployed(ctx context.Context, pr string) (bool, error)
	// CI is the state of the checks of the pull request's head commit (the one it was merged with, once merged):
	// CISuccess, CIFailure, CIPending or CINone, and that commit.
	CI(ctx context.Context, pr string) (state, commit string, err error)
	Fetch(ctx context.Context, url string) (Page, error)
}

// Result is what one check found. Evidence never carries the body of a page.
type Result struct {
	Check
	State    string
	Evidence string
	At       time.Time
}

// Run runs every check, in order, through ch. now is the clock.
func Run(ctx context.Context, checks []Check, ch Channels, now func() time.Time) []Result {
	out := make([]Result, 0, len(checks))
	for _, c := range checks {
		state, evidence := run(ctx, c, ch)
		out = append(out, Result{Check: c, State: state, Evidence: textfmt.Clip(evidence, 400), At: now().UTC()})
	}
	return out
}

func run(ctx context.Context, c Check, ch Channels) (state, evidence string) {
	if c.Kind == KindHTTP {
		return runHTTP(ctx, c, ch)
	}
	prs := ch.PullRequests(ctx)
	if len(prs) == 0 {
		return Cannot, c.Kind + ": no pull request is recorded for this to-do"
	}
	var notes []string
	met := true
	for _, pr := range prs {
		ok, note, err := onePR(ctx, c.Kind, pr, ch)
		if err != nil {
			return Cannot, fmt.Sprintf("%s: %s cannot be checked: %v", c.Kind, pr, err)
		}
		met = met && ok
		notes = append(notes, pr+" "+note)
	}
	if met {
		return Met, c.Kind + ": " + strings.Join(notes, "; ")
	}
	return NotMet, c.Kind + ": " + strings.Join(notes, "; ")
}

// onePR checks one pull request; note says what was found, which commit's checks included.
func onePR(ctx context.Context, kind, pr string, ch Channels) (ok bool, note string, err error) {
	say := func(ok bool, yes, no string) string {
		if ok {
			return yes
		}
		return no
	}
	switch kind {
	case KindPRMerged:
		ok, err = ch.Merged(ctx, pr)
		return ok, say(ok, "is merged", "is not merged"), err
	case KindDeployed:
		ok, err = ch.Deployed(ctx, pr)
		return ok, say(ok, "is in the running deployment", "is not in the running deployment"), err
	case KindCIGreen:
		state, commit, err := ch.CI(ctx, pr)
		if err != nil {
			return false, "", err
		}
		if len(commit) > 8 {
			commit = commit[:8]
		}
		return state == CISuccess, map[string]string{CISuccess: "has green checks", CIFailure: "has failing checks", CIPending: "has checks still running", CINone: "has no checks"}[state] + " (commit " + commit + ")", nil
	}
	return false, "", fmt.Errorf("unknown kind %q", kind)
}

func runHTTP(ctx context.Context, c Check, ch Channels) (state, evidence string) {
	spec, err := ParseHTTP(c.Arg)
	if err != nil {
		return Cannot, "http: " + err.Error()
	}
	page, err := ch.Fetch(ctx, spec.URL)
	if err != nil {
		return Cannot, fmt.Sprintf("http: %s cannot be fetched: %v", spec.URL, err)
	}
	evidence = fmt.Sprintf("http: GET %s answered %d (expected %d)", spec.URL, page.Status, spec.Status)
	state = Met
	if page.Status != spec.Status {
		state = NotMet
	}
	if spec.Contains != "" {
		found := bytes.Contains(page.Body, []byte(spec.Contains))
		evidence += "; required text " + map[bool]string{true: "found", false: "not found"}[found]
		if !found {
			state = NotMet
		}
	}
	return state, evidence
}

// Criterion is the verdict on one criterion: all its checks together.
type Criterion struct {
	Criterion string
	State     string // Met, NotMet or Cannot
	Evidence  string
	At        time.Time
}

// Fold gives each criterion of doneWhen that has a result its verdict: not met when any check is not met, else cannot
// when any could not be checked, else met. A criterion with no result is left out.
func Fold(doneWhen []string, results []Result) []Criterion {
	var out []Criterion
	for _, text := range doneWhen {
		v := Criterion{Criterion: text}
		var parts []string
		for _, r := range results {
			if r.Criterion != text {
				continue
			}
			parts = append(parts, r.Evidence)
			v.At = r.At
			switch {
			case v.State == "" || r.State == NotMet || r.State == Cannot && v.State == Met:
				v.State = r.State
			}
		}
		if v.State == "" {
			continue
		}
		v.Evidence = strings.Join(parts, "; ")
		out = append(out, v)
	}
	return out
}

// AllMet says every criterion of doneWhen has a declared check and every result is met: the hub may accept.
func AllMet(doneWhen []string, checks []Check, results []Result) bool {
	if !Covered(doneWhen, checks) || len(results) < len(checks) {
		return false
	}
	for _, r := range results {
		if r.State != Met {
			return false
		}
	}
	return true
}

// Lines is one line per criterion: its number, state and evidence, for the reply to a verify call.
func Lines(doneWhen []string, verdicts []Criterion) string {
	var b strings.Builder
	for _, v := range verdicts {
		for i, text := range doneWhen {
			if text == v.Criterion {
				fmt.Fprintf(&b, "%d. %s: %s\n", i+1, strings.ReplaceAll(v.State, "_", " "), v.Evidence)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// OwnerWait is the refusal of an acceptance while the work stopped on a decision only the owner can make: closing it
// would lose that decision. reason is the done judge's, "" when it gave none.
func OwnerWait(id, reason string) error {
	why := ""
	if reason != "" {
		why = fmt.Sprintf(" (the done judge: %q)", reason)
	}
	return errors.New("task " + id + " stopped on a decision only the owner can make" + why + ": accepting it would close the task and lose " +
		"that decision. Review it needs_user as a decision they can answer (why_owner, reason, recommend, options, " +
		"cost_of_waiting); if the owner already decided, send their answer to its agent with agent_send and review the " +
		"work it does next; or cancelled when it is no longer wanted")
}

// Gates is the order the gates of an acceptance run in, as far as they read a ledger: the owner's wait first, then
// the criteria. open says the to-do is not closed yet; waits and reason are the owner wait the ledger holds;
// incomplete is why the criteria are not all checked (nil when they are).
func Gates(id string, open, waits bool, reason string, incomplete error) error {
	if open && waits {
		return OwnerWait(id, reason)
	}
	return incomplete
}
