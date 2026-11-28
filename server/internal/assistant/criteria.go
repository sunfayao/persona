package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/acceptance"
	"github.com/sunfayao/persona/server/internal/assistant/criteria"
	"github.com/sunfayao/persona/server/internal/db"
)

// criterionReport is an agent's reading of one criterion by its number (criteria.Report).
type criterionReport = criteria.Report

// applyChecks records only the criteria actually checked, without deriving any
// task verdict from their count. Unmatched checks remain in the review history.
func (v *Supervision) applyChecks(checks []ReviewCheck, at time.Time, by string) {
	v.Criteria = db.AlignTodoCriteria(v.DoneWhen, v.Criteria)
	for _, check := range checks {
		for i, text := range v.DoneWhen {
			if !criteria.Same(check.Criterion, text) {
				continue
			}
			state := "not_met"
			if check.Met {
				state = "met"
			}
			v.Criteria[i] = db.TodoCriterion{Criterion: text, State: state, Evidence: check.Evidence, At: &at, By: by}
			break
		}
	}
}

func (s *Service) recordCriterionReports(ctx context.Context, t Todo, machine, session string, reports []criterionReport, at time.Time) error {
	if len(reports) == 0 {
		return nil
	}
	moved := false // a reading changed: progress (supervise_progress.go)
	_, changed, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		moved = false
		// A concurrent criteria edit must not attach this report to a different entry.
		cur, ok := s.taskOf(ctx, machine, session)
		if !ok || cur.ID != t.ID || cur.DoneAt != nil || !slices.Equal(v.DoneWhen, t.Supervision.DoneWhen) {
			return false
		}
		v.Criteria = db.AlignTodoCriteria(v.DoneWhen, v.Criteria)
		for _, r := range reports {
			text := v.DoneWhen[r.Number-1]
			if old := v.Criteria[r.Number-1]; old.State != r.State || old.Evidence != r.Evidence {
				moved = true
			}
			v.Criteria[r.Number-1] = db.TodoCriterion{Criterion: text, State: r.State, Evidence: r.Evidence,
				At: &at, By: "agent:" + machine + "/" + session}
		}
		return true
	})
	if err == nil && !changed {
		return fmt.Errorf("task or done_when changed: read the current task and report again")
	}
	if err == nil && moved {
		s.progressMade(machine, session, at)
	}
	return err
}

// ---- typed checks the hub runs (assistant/acceptance) ----

// reviewerHub marks a review the hub wrote itself (TaskReview.By): every criterion had a typed check and all were met.
const reviewerHub = "hub"

type hubActingKey struct{}

// hubActing: the call is the hub's own acceptance, not a conversation's.
func hubActing(ctx context.Context) bool { return ctx.Value(hubActingKey{}) != nil }

// hubChannels are the ways a to-do's checks read the world: its pull requests on record, the forge, the running
// deployment and an HTTP GET.
type hubChannels struct {
	s  *Service
	id string
}

func (c hubChannels) PullRequests(ctx context.Context) []string {
	t, _ := c.s.todoNow(c.id)
	linked, _, _ := c.s.todoLinkedDeliveryRefs(ctx, c.id)
	var out []string
	for _, u := range append(append([]string{t.PRURL}, t.PRs...), linked...) {
		if ciPRURL.MatchString(u) && !slices.ContainsFunc(out, func(o string) bool { return triageNormalizePRURL(o) == triageNormalizePRURL(u) }) {
			out = append(out, u)
		}
	}
	return out
}

// forgeReason says why the forge did not answer: no GitHub access is named as such.
func forgeReason(err error) error {
	if errors.Is(err, errGitHubUnconfigured) {
		return fmt.Errorf("no GitHub access (%w)", err)
	}
	return err
}

func prParts(pr string) (repo string, number int) {
	m := ciPRURL.FindStringSubmatch(pr)
	number, _ = strconv.Atoi(m[2])
	return m[1], number
}

func (c hubChannels) Merged(ctx context.Context, pr string) (bool, error) {
	r := c.s.checkPRDeployment(ctx, pr, true)
	switch {
	case r.Merge != "" || r.State == deployOut:
		return true, nil
	case r.State == deployUnmerged || r.NotMerged:
		return false, nil
	case r.Err != nil:
		return false, forgeReason(r.Err)
	}
	repo, n := prParts(pr)
	merged, _, err := c.s.forge().PRMerge(ctx, repo, n)
	return merged, forgeReason(err)
}

func (c hubChannels) Deployed(ctx context.Context, pr string) (bool, error) {
	r := c.s.checkPRDeployment(ctx, pr, false)
	switch r.State {
	case deployIn:
		return true, nil
	case deployOut, deployUnmerged, deployNotIn:
		return false, nil
	}
	return false, forgeReason(cmp.Or(r.Err, errors.New("the running deployment does not say which commit it was built from, or the pull request is not in its repository")))
}

// prHeadChecksForge is the forge's answer for the checks of a pull request's head commit, open or merged.
type prHeadChecksForge interface {
	PRHeadChecks(ctx context.Context, repo string, number int) (TodoPRStatus, error)
}

func (c hubChannels) CI(ctx context.Context, pr string) (state, commit string, err error) {
	f, ok := c.s.forge().(prHeadChecksForge)
	if !ok {
		return "", "", errors.New("the hub has no channel for CI state")
	}
	repo, n := prParts(pr)
	st, err := f.PRHeadChecks(ctx, repo, n)
	switch {
	case err != nil:
		return "", "", forgeReason(err)
	case st.State == "closed":
		return "", "", errors.New("the pull request is closed without a merge")
	}
	return st.Checks, st.HeadOID, nil
}

func (c hubChannels) Fetch(ctx context.Context, url string) (acceptance.Page, error) {
	return acceptance.HTTP{}.Fetch(ctx, url)
}

// runHubChecks runs t's declared checks, records each criterion's result on its ledger as the hub's own (a check that
// cannot be made leaves the criterion not met, with the reason as evidence) and says whether the hub may accept.
func (s *Service) runHubChecks(ctx context.Context, t Todo) (verdicts []acceptance.Criterion, all bool, err error) {
	v := t.Supervision
	if v == nil || len(v.Checks) == 0 {
		return nil, false, nil
	}
	run, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	results := acceptance.Run(run, v.Checks, hubChannels{s, t.ID}, time.Now)
	verdicts = acceptance.Fold(v.DoneWhen, results)
	_, _, err = s.updateSupervision(ctx, t.ID, func(cur *Supervision) bool {
		if !slices.Equal(cur.DoneWhen, v.DoneWhen) {
			return false // the scope changed while the checks ran: they read the old criteria
		}
		cur.Criteria = db.AlignTodoCriteria(cur.DoneWhen, cur.Criteria)
		for _, c := range verdicts {
			at, state := c.At, "not_met"
			if c.State == acceptance.Met {
				state = "met"
			}
			cur.Criteria[slices.Index(cur.DoneWhen, c.Criterion)] = db.TodoCriterion{Criterion: c.Criterion, State: state, Evidence: c.Evidence, At: &at, By: reviewerHub}
		}
		return true
	})
	return verdicts, acceptance.AllMet(v.DoneWhen, v.Checks, results), err
}

// verifyChecks is task_review verify:true: the hub runs t's checks again and answers one line per criterion. It writes
// no verdict.
func (s *Service) verifyChecks(ctx context.Context, t Todo) (string, error) {
	verdicts, _, err := s.runHubChecks(ctx, t)
	switch {
	case err != nil:
		return "", err
	case len(verdicts) == 0:
		return "", fmt.Errorf("task %s declares no typed checks: declare them with checks on task_plan or task_scope", t.ID)
	}
	return "The hub ran the declared checks (no verdict was written; criteria without a check are not listed):\n" + acceptance.Lines(t.Supervision.DoneWhen, verdicts), nil
}

// hubDue says the hub has typed checks to run for t's review this round: t is waiting for review and has not been run
// since it began.
func hubDue(t Todo) bool {
	v := t.Supervision
	return v != nil && t.DoneAt == nil && len(v.Checks) > 0 && v.State == reviewPending &&
		!slices.ContainsFunc(v.Criteria, func(c db.TodoCriterion) bool {
			return c.By == reviewerHub && c.At != nil && v.PendingSince != nil && !c.At.Before(*v.PendingSince)
		})
}

// releaseHubHold lets a review that waited for the hub's run go on to its supervisor.
func (s *Service) releaseHubHold(id string) {
	b := &s.supervise
	b.mu.Lock()
	defer b.mu.Unlock()
	if g, ok := b.pending[id+"|"+sigReview]; ok && !g.NotBefore.IsZero() {
		g.NotBefore = time.Time{}
		b.pending[id+"|"+sigReview] = g
		s.scheduleDigestLocked(time.Now())
	}
}

// hubReview runs when a to-do enters pending review. With typed checks declared it runs them; when every criterion has
// one and all are met, and the owner's own confirmation is not needed, it accepts the to-do itself, with no model turn,
// through the review path its supervisor would use. Otherwise the review goes on to its supervisor, with the hub's
// evidence on the ledger.
func (s *Service) hubReview(ctx context.Context, id string) {
	defer s.releaseHubHold(id)
	t, ok := s.todoNow(id)
	if !ok || !hubDue(t) {
		return
	}
	verdicts, all, err := s.runHubChecks(ctx, t)
	if err != nil || !all || s.ownerVerifies(t) {
		if err != nil {
			s.logf("task %s: hub checks: %v", id, err)
		}
		return
	}
	checks := make([]ReviewCheck, len(verdicts))
	for i, c := range verdicts {
		checks[i] = ReviewCheck{Criterion: c.Criterion, Met: true, Evidence: c.Evidence}
	}
	args, _ := json.Marshal(map[string]any{"todo_id": id, "verdict": reviewAccepted, "outcome": outcomeDone, "checks": checks,
		"note": "accepted by the hub: every criterion has a typed check and all of them are met"})
	ctx = context.WithValue(ctx, hubActingKey{}, true)
	if lead, supervised, reachable := s.leaderSupervisor(t); supervised && reachable {
		_, err = s.leaderReview(ctx, lead, args)
	} else {
		_, err = s.toolTaskReview(ctx, t.Supervision.Supervisor, args)
	}
	if err != nil {
		s.logf("task %s: hub acceptance: %v", id, err)
	}
}

// declaredChecks are the checks a call declares and who declares them.
type declaredChecks struct {
	List []acceptance.Declared
	By   acceptance.Role
}

// setChecks keeps declared on v, on the criteria v has now.
func (v *Supervision) setChecks(d declaredChecks) {
	if added, err := acceptance.Declare(v.DoneWhen, d.List, d.By); err == nil && len(added) > 0 {
		v.Checks = acceptance.Merge(v.DoneWhen, v.Checks, added)
	}
}

// mayReview says conversation self may review t: main, the background supervisor of it, a capable side conversation
// that supervises it, the conversation above a task leader, or the hub itself.
func (s *Service) mayReview(ctx context.Context, self string, t Todo, isMain, background bool) bool {
	capableRuntime := t.Supervision != nil && (t.Supervision.Supervisor == self || t.Supervision.Above == self) && t.Origin == self && s.runtimeWithSupervisionPolicy(ctx, s.settings(ctx).MachineID, self)
	return hubActing(ctx) || isMain || background || capableRuntime || s.mayReviewForLeader(ctx, self, t)
}

// verifyReview is task_review verify:true for whoever may review the to-do.
func (s *Service) verifyReview(ctx context.Context, self, id string) (string, error) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return "", err
	}
	t, ok := s.todoNow(strings.TrimSpace(id))
	if !ok {
		return "", fmt.Errorf("no task %q", id)
	}
	if t.Origin == "" || !s.mayReview(ctx, self, t, s.isMain(ctx, self), s.backgroundSupervises(ctx, self, t)) {
		return "", fmt.Errorf("task %s was not handed out from this conversation", t.ID)
	}
	return s.verifyChecks(ctx, t)
}

// planRole is who declares checks in a task_plan: a supervisor Agent plans with its scoped tool, the main conversation
// with its own.
func planRole(supervisor bool) acceptance.Role {
	if supervisor {
		return acceptance.Supervisor
	}
	return acceptance.Main
}

// alreadyAccepted refuses a second acceptance of a closed to-do when either one is the hub's: of the hub's and a
// reviewer's at the same moment exactly one takes effect, and the other is told so.
func alreadyAccepted(ctx context.Context, t Todo) error {
	v := t.Supervision
	if t.DoneAt == nil || v == nil || len(v.Reviews) == 0 {
		return nil
	}
	if last := v.Reviews[len(v.Reviews)-1]; last.Verdict == reviewAccepted && (hubActing(ctx) || last.By == reviewerHub) {
		return fmt.Errorf("task %s is already accepted: nothing more to record", t.ID)
	}
	return nil
}
