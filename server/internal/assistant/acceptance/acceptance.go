// Package acceptance is the pure logic of typed acceptance: checks a to-do declares on its criteria and the hub runs
// itself, the results they leave, and the gates every acceptance passes. It runs a check through the channels the
// caller gives it, takes a clock, keeps no state and imports neither the assistant root nor a sibling domain.
package acceptance

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The closed set of check kinds. A command check is not among them: the hub has no way to run a command that the
// owner has not confirmed (machine_exec asks per run when the owner says so, and never runs for the hub itself).
const (
	KindPRMerged = "pr_merged" // every pull request on record for the to-do is merged
	KindCIGreen  = "ci_green"  // every one has its checks green
	KindDeployed = "deployed"  // the running deployment contains every one
	KindHTTP     = "http"      // a GET answers as the arg says
)

// Check is one declared check as the ledger keeps it. It belongs to the criterion with exactly this text.
type Check struct {
	Criterion string `json:"criterion"`
	Kind      string `json:"kind"`
	Arg       string `json:"arg,omitempty"`
}

// Declared is a check as a tool call names it: by the criterion's one-based number.
type Declared struct {
	Criterion int    `json:"criterion"`
	Kind      string `json:"kind"`
	Arg       string `json:"arg,omitempty"`
}

// Role is who declares: it decides which kinds may be.
type Role int

const (
	Main       Role = iota // the main conversation, and any conversation that supervises the to-do: every kind
	Supervisor             // a to-do's supervisor Agent: the kinds that read a pull request and the deployment
	Worker                 // a working Agent: none
)

// MostChecks is the most checks a to-do keeps.
const MostChecks = 12

func (r Role) allows(kind string) bool {
	switch r {
	case Main:
		return slices.Contains([]string{KindPRMerged, KindCIGreen, KindDeployed, KindHTTP}, kind)
	case Supervisor:
		return slices.Contains([]string{KindPRMerged, KindCIGreen, KindDeployed}, kind)
	}
	return false
}

// Declare turns what a call declared on doneWhen into checks with the criteria's text, or refuses with one sentence.
func Declare(doneWhen []string, in []Declared, by Role) ([]Check, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if by == Worker {
		return nil, errors.New("checks: a working Agent cannot declare checks; its supervisor does")
	}
	if len(in) > MostChecks {
		return nil, fmt.Errorf("checks: at most %d checks", MostChecks)
	}
	out := make([]Check, 0, len(in))
	for i, d := range in {
		d.Kind, d.Arg = strings.TrimSpace(d.Kind), strings.TrimSpace(d.Arg)
		switch {
		case d.Criterion < 1 || d.Criterion > len(doneWhen):
			return nil, fmt.Errorf("checks[%d]: criterion %d is not a number from 1 to %d", i+1, d.Criterion, len(doneWhen))
		case !Main.allows(d.Kind):
			return nil, fmt.Errorf("checks[%d]: kind must be pr_merged, ci_green, deployed or http", i+1)
		case !by.allows(d.Kind):
			return nil, fmt.Errorf("checks[%d]: a supervisor Agent may declare only pr_merged, ci_green or deployed; ask the main conversation for %s", i+1, d.Kind)
		case d.Kind == KindHTTP:
			if _, err := ParseHTTP(d.Arg); err != nil {
				return nil, fmt.Errorf("checks[%d]: %w", i+1, err)
			}
		case d.Arg != "":
			return nil, fmt.Errorf("checks[%d]: %s takes no arg", i+1, d.Kind)
		}
		c := Check{Criterion: doneWhen[d.Criterion-1], Kind: d.Kind, Arg: d.Arg}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Merge is the checks a ledger keeps after added were declared on doneWhen: the criteria added names get exactly the
// added checks, the others keep theirs, and a check of a criterion that is not in doneWhen goes.
func Merge(doneWhen []string, current, added []Check) []Check {
	named := map[string]bool{}
	for _, c := range added {
		named[c.Criterion] = true
	}
	out := slices.Clone(Align(doneWhen, current))
	out = slices.DeleteFunc(out, func(c Check) bool { return named[c.Criterion] })
	out = append(out, added...)
	if len(out) > MostChecks {
		out = out[:MostChecks]
	}
	return out
}

// Align keeps the checks whose criterion is in doneWhen, by its exact text: a criterion that was dropped or edited
// takes its checks with it. The result owns its slice.
func Align(doneWhen []string, checks []Check) []Check {
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		if slices.Contains(doneWhen, c.Criterion) {
			out = append(out, c)
		}
	}
	return out
}

// Covered says every criterion has a declared check. A to-do with no criteria is not covered: nothing is typed.
func Covered(doneWhen []string, checks []Check) bool {
	if len(doneWhen) == 0 {
		return false
	}
	for _, text := range doneWhen {
		if !slices.ContainsFunc(checks, func(c Check) bool { return c.Criterion == text }) {
			return false
		}
	}
	return true
}

// HTTPSpec is what an http check expects.
type HTTPSpec struct {
	URL      string
	Status   int    // 200 when the arg gives none
	Contains string // text the body must contain; "" for none
}

const maxContains = 200

// ParseHTTP reads an http check's arg: the URL, then optionally the expected status, then optionally text the body
// must contain (the rest of the arg).
func ParseHTTP(arg string) (HTTPSpec, error) {
	rest := strings.TrimSpace(arg)
	raw, rest, _ := strings.Cut(rest, " ")
	u, err := url.Parse(raw)
	switch {
	case raw == "" || err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https":
		return HTTPSpec{}, errors.New("an http check's arg starts with an http or https URL, then optionally the expected status and text the body must contain")
	case u.User != nil:
		return HTTPSpec{}, errors.New("an http check sends no credentials: leave them out of the URL")
	case len(raw) > 2000:
		return HTTPSpec{}, errors.New("an http check's URL is at most 2000 characters")
	}
	spec := HTTPSpec{URL: raw, Status: 200}
	rest = strings.TrimSpace(rest)
	if field, tail, _ := strings.Cut(rest, " "); field != "" {
		if n, err := strconv.Atoi(field); err == nil {
			if n < 100 || n > 599 {
				return HTTPSpec{}, errors.New("an http check's expected status is from 100 to 599")
			}
			spec.Status, rest = n, strings.TrimSpace(tail)
		}
	}
	if utf8.RuneCountInString(rest) > maxContains {
		return HTTPSpec{}, fmt.Errorf("an http check's text is at most %d characters", maxContains)
	}
	spec.Contains = rest
	return spec, nil
}
