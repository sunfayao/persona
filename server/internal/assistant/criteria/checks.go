package criteria

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sunfayao/persona/server/internal/textfmt"
)

// Check is a supervisor's reading of one criterion in task_review. It names the criterion by its text, by its
// one-based number in the current done_when, or by both (which must agree). A stored review keeps the text only.
type Check struct {
	Number    int    `json:"number,omitempty"`
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
	Evidence  string `json:"evidence,omitempty"`
}

// ResolveChecks gives every check its criterion text from the current doneWhen and clears its number. usedNumber
// says some check was named by number, so the review belongs to the list it was prepared against.
func ResolveChecks(doneWhen []string, checks []Check) (out []Check, usedNumber bool, err error) {
	out = make([]Check, len(checks))
	for i, c := range checks {
		if c.Number != 0 {
			usedNumber = true
		}
		if c.Criterion, err = Resolve(doneWhen, c.Number, c.Criterion); err != nil {
			return nil, false, fmt.Errorf("check %d: %w", i+1, err)
		}
		c.Number = 0
		out[i] = c
	}
	return out, usedNumber, nil
}

// ErrChanged refuses a review whose numbers were read from a list that has changed since.
var ErrChanged = errors.New("done_when changed after you read it, so the numbers may name other criteria: read the task and review again; nothing was written")

// Complete refuses an ordinary acceptance whose checks do not prove every current criterion: exactly one met check
// with evidence for each. Callers use it only for accepted/done reviews; checks are resolved already.
func Complete(doneWhen []string, checks []Check) error {
	if len(doneWhen) == 0 {
		return nil
	}
	matched := make([]int, len(doneWhen))
	for i, check := range checks {
		criterion := Fit(check.Criterion)
		at, matches := -1, 0
		for j, current := range doneWhen {
			if Same(criterion, current) {
				at, matches = j, matches+1
			}
		}
		switch {
		case matches == 0:
			return fmt.Errorf("accepted: check %d names unknown current done_when %q; use the current criteria and exactly one met check with non-empty evidence for each; nothing was written", i+1, criterion)
		case matches > 1:
			return fmt.Errorf("accepted: check %d criterion %q matches more than one current done_when entry; nothing was written", i+1, criterion)
		case matched[at] != 0:
			return fmt.Errorf("accepted: current done_when %q has duplicate checks; supply exactly one met check with non-empty evidence for every criterion; nothing was written", doneWhen[at])
		case !check.Met:
			return fmt.Errorf("accepted: current done_when %q is not met; use rework for what is missing; nothing was written", doneWhen[at])
		case strings.TrimSpace(check.Evidence) == "":
			return fmt.Errorf("accepted: current done_when %q needs non-empty evidence; nothing was written", doneWhen[at])
		}
		matched[at] = i + 1
	}
	for i, current := range doneWhen {
		if matched[i] == 0 {
			return fmt.Errorf("accepted: current done_when %q has no check; supply exactly one met check with non-empty evidence for every criterion; nothing was written", current)
		}
	}
	return nil
}

// Report uses the one-based position the agent saw in done_when. Text matching remains the persisted identity, so
// later reordering cannot move evidence.
type Report struct {
	Number   int    `json:"number"`
	State    string `json:"state"`
	Evidence string `json:"evidence"`
}

// CleanReports validates an agent's reports against its done_when.
func CleanReports(reports []Report, doneWhen []string) ([]Report, error) {
	out := make([]Report, 0, len(reports))
	seen := map[int]bool{}
	for _, r := range reports {
		if r.Number < 1 || r.Number > len(doneWhen) {
			return nil, fmt.Errorf("criteria: number must be between 1 and %d", len(doneWhen))
		}
		if seen[r.Number] {
			return nil, fmt.Errorf("criteria: number %d appears twice", r.Number)
		}
		seen[r.Number] = true
		switch r.State {
		case "pending", "met", "not_met":
		default:
			return nil, fmt.Errorf("criteria: state must be pending, met or not_met")
		}
		r.Evidence = textfmt.Clip(strings.TrimSpace(r.Evidence), 600)
		if r.State != "pending" && r.Evidence == "" {
			return nil, fmt.Errorf("criteria: number %d needs evidence", r.Number)
		}
		out = append(out, r)
	}
	return out, nil
}
