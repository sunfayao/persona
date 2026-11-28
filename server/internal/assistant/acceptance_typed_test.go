package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/acceptance"
	"github.com/sunfayao/persona/server/internal/assistant/criteria"
	"github.com/sunfayao/persona/server/internal/db"
)

// typedService is a supervised to-do (criteria 1 "PR merged", 2 "screenshots") with PR 9 of o/r on record, the hub
// running oldHub, the forge as given and delivery_gate live.
func typedService(t *testing.T, forge prForge, withPR bool) (*Service, *fakeHub, Todo) {
	t.Helper()
	s, f, todo := newSupervisedService(t)
	s.deploy.forge = forge
	s.deploy.hubBuild = (&runningHub{commit: oldHub}).build
	s.deploy.listPaths = []string{}
	deliveryOn(t, s)
	if withPR {
		s.setTaskStage(context.Background(), todo.ID, "review", "https://github.com/o/r/pull/9")
	}
	return s, f, todo
}

func declareChecks(s *Service, id, checks string) (string, error) {
	raw := fmt.Sprintf(`{"todo_id":%q,"change":"typed acceptance","why":"the hub can check it","checks":%s}`, id, checks)
	return s.toolTaskScope(context.Background(), "conv1", json.RawMessage(raw))
}

func ledgerOf(s *Service, id string) *Supervision { return todoNow(s, id).Supervision }

const bothTyped = `[{"criterion":1,"kind":"pr_merged"},{"criterion":2,"kind":"deployed"}]`

func mergedAndLive() *fakeForge {
	return &fakeForge{merged: true, sha: mergeSHA, contains: map[string]bool{oldHub: true}}
}

func TestTheHubRunsDeclaredChecksAndRecordsThemAsItsOwnWithNoVerdict(t *testing.T) {
	s, f, todo := typedService(t, &fakeForge{merged: true, sha: mergeSHA, contains: map[string]bool{oldHub: false}}, true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	if got := ledgerOf(s, todo.ID).Checks; len(got) != 2 || got[0].Kind != acceptance.KindPRMerged || got[0].Criterion != "PR 合并到 master" {
		t.Fatalf("checks: %+v", got)
	}
	res, err := s.toolTaskReview(context.Background(), "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","verify":true}`))
	if err != nil || !strings.Contains(res, "1. met: ") || !strings.Contains(res, "2. not met: ") || !strings.Contains(res, "not in the running deployment") {
		t.Fatalf("verify: %q %v", res, err)
	}
	v := ledgerOf(s, todo.ID)
	if v.Criteria[0].State != "met" || v.Criteria[0].By != "hub" || v.Criteria[1].State != "not_met" || v.Criteria[1].By != "hub" || v.Criteria[1].At == nil {
		t.Fatalf("criteria: %+v", v.Criteria)
	}
	if len(v.Reviews) != 0 || v.State != "" || todoNow(s, todo.ID).DoneAt != nil || len(f.sends) != 0 {
		t.Fatalf("verify wrote a verdict: %+v", v)
	}
	// The same call on a to-do with no declared checks says so and writes nothing either.
	s2, _, plain := typedService(t, mergedAndLive(), true)
	if _, err := s2.toolTaskReview(context.Background(), "conv1", json.RawMessage(`{"todo_id":"`+plain.ID+`","verify":true}`)); err == nil || !strings.Contains(err.Error(), "declares no typed checks") {
		t.Fatalf("no checks: %v", err)
	}
}

func TestACheckThatCannotBeMadeLeavesTheCriterionNotMetAndNamesWhy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		forge  prForge
		withPR bool
		want   string
	}{
		{"no GitHub access", &fakeForge{err: fmt.Errorf("%w: no token", errGitHubUnconfigured)}, true, "no GitHub access"},
		{"no pull request on record", mergedAndLive(), false, "no pull request is recorded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, todo := typedService(t, tc.forge, tc.withPR)
			if _, err := declareChecks(s, todo.ID, `[{"criterion":1,"kind":"pr_merged"}]`); err != nil {
				t.Fatal(err)
			}
			res, err := s.toolTaskReview(context.Background(), "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","verify":true}`))
			if err != nil || !strings.Contains(res, "1. cannot: ") || !strings.Contains(res, tc.want) {
				t.Fatalf("%q %v", res, err)
			}
			if c := ledgerOf(s, todo.ID).Criteria[0]; c.State != "not_met" || c.By != "hub" || !strings.Contains(c.Evidence, tc.want) {
				t.Fatalf("a cannot is never met: %+v", c)
			}
		})
	}
}

func TestEveryCriterionTypedAndMetClosesTheTodoWithNoModelTurn(t *testing.T) {
	s, f, todo := typedService(t, mergedAndLive(), true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.tasks.outcomeRuns.Wait()
	got := todoNow(s, todo.ID)
	if got.DoneAt == nil || got.Supervision.state(got) != reviewAccepted {
		t.Fatalf("not closed: %+v", got.Supervision)
	}
	last := got.Supervision.Reviews[len(got.Supervision.Reviews)-1]
	if last.Verdict != reviewAccepted || last.By != "hub" || len(last.Checks) != 2 || !supervisorVerified(got) {
		t.Fatalf("review: %+v", last)
	}
	for _, c := range got.Supervision.Criteria {
		if c.State != "met" || c.By != "hub" {
			t.Fatalf("criteria: %+v", got.Supervision.Criteria)
		}
	}
	if len(f.sends) != 0 {
		t.Fatalf("a model turn was started: %v", f.sends)
	}
}

func TestOneFailingCheckSendsTheReviewOnWithTheHubsEvidence(t *testing.T) {
	s, f, todo := typedService(t, &fakeForge{merged: true, sha: mergeSHA, contains: map[string]bool{oldHub: false}}, true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.tasks.outcomeRuns.Wait()
	got := todoNow(s, todo.ID)
	if got.DoneAt != nil || got.Supervision.State != reviewPending || len(got.Supervision.Reviews) != 0 {
		t.Fatalf("closed or reviewed: %+v", got.Supervision)
	}
	if c := got.Supervision.Criteria; c[0].State != "met" || c[1].State != "not_met" || c[1].By != "hub" || !strings.Contains(c[1].Evidence, "deployment") {
		t.Fatalf("the hub's evidence: %+v", c)
	}
	waitFor(t, "the review reaches its supervisor", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sends) > 0 })
}

func TestAnOwnersConfirmationKeepsAHighPriorityTodoOutOfTheHubsAcceptance(t *testing.T) {
	s, _, todo := typedService(t, mergedAndLive(), true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	high := db.PriorityHigh
	if _, err := s.editTodo(context.Background(), todo.ID, db.TodoEdit{Priority: &high}); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.tasks.outcomeRuns.Wait()
	if got := todoNow(s, todo.ID); got.DoneAt != nil || got.Supervision.Criteria[0].By != "hub" {
		t.Fatalf("checked but not accepted: %+v", got.Supervision)
	}
}

func TestADroppedOrEditedCriterionTakesItsCheckWithIt(t *testing.T) {
	s, _, todo := typedService(t, mergedAndLive(), true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.toolTaskScope(ctx, "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"reword the screenshots","why":"owner","edit":[{"number":2,"text":"screenshots in the PR"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := ledgerOf(s, todo.ID).Checks; len(got) != 1 || got[0].Criterion != "PR 合并到 master" {
		t.Fatalf("an edited criterion keeps its check: %+v", got)
	}
	if _, err := s.toolTaskScope(ctx, "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"drop the first criterion","why":"owner","drop":[1]}`)); err != nil {
		t.Fatal(err)
	}
	if got := ledgerOf(s, todo.ID).Checks; len(got) != 0 {
		t.Fatalf("a dropped criterion keeps its check: %+v", got)
	}
}

func TestADeclarationThatIsRefusedWritesNothing(t *testing.T) {
	s, _, todo := typedService(t, mergedAndLive(), true)
	logged := len(todoBrief(s, todo.ID).Log)
	for _, bad := range []string{
		`[{"criterion":3,"kind":"pr_merged"}]`,
		`[{"criterion":1,"kind":"command","arg":"make test"}]`,
		`[{"criterion":1,"kind":"pr_merged"},{"criterion":2,"kind":"http","arg":"ftp://host/x"}]`,
	} {
		if _, err := declareChecks(s, todo.ID, bad); err == nil || !strings.Contains(err.Error(), "nothing was written") {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if got := ledgerOf(s, todo.ID); len(got.Checks) != 0 || len(todoBrief(s, todo.ID).Log) != logged {
		t.Fatalf("written: %+v", got)
	}
}

func todoBrief(s *Service, id string) TaskBrief { b, _ := s.briefOfTodo(id); return b }

func TestASupervisorAgentDeclaresOnlyThePullRequestKinds(t *testing.T) {
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	ctx := context.Background()
	for _, kind := range []string{`{"criterion":1,"kind":"http","arg":"https://h.test/health"}`, `{"criterion":1,"kind":"command","arg":"make"}`} {
		res := s.mcpCall(ctx, "server", "s2", "task_scope", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"typed","why":"hub","checks":[`+kind+`]}`), false)
		if !res.IsError || !strings.Contains(res.Content[0].Text, "kind must be") && !strings.Contains(res.Content[0].Text, "only pr_merged, ci_green or deployed") {
			t.Fatalf("%s: %+v", kind, res)
		}
	}
	res := s.mcpCall(ctx, "server", "s2", "task_scope", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"typed","why":"hub","checks":[{"criterion":1,"kind":"ci_green"}]}`), false)
	if res.IsError || len(ledgerOf(s, todo.ID).Checks) != 1 {
		t.Fatalf("ci_green: %+v", res)
	}
	// Its verify call runs the hub's checks and writes no verdict.
	res = s.mcpCall(ctx, "server", "s2", "task_review", json.RawMessage(`{"todo_id":"`+todo.ID+`","verify":true}`), false)
	if res.IsError || !strings.Contains(res.Content[0].Text, "1. cannot: ") || len(ledgerOf(s, todo.ID).Reviews) != 0 {
		t.Fatalf("verify: %+v", res)
	}
}

func TestTheSupervisorAgentsAcceptanceOfAMemberSupervisedTodoPassesTheMainGates(t *testing.T) {
	s, f, todo := autoRequest(t)
	ctx := context.Background()
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	forge := &fakeForge{}
	s.deploy.forge = forge
	s.deploy.hubBuild = (&runningHub{commit: oldHub}).build
	s.deploy.listPaths = []string{}
	review := func(checks string) acceptedReply {
		res := s.mcpCall(ctx, "server", "s2", "task_review", json.RawMessage(`{"todo_id":"`+todo.ID+`","verdict":"accepted","checks":`+checks+`}`), false)
		return acceptedReply{res.IsError, res.Content[0].Text}
	}
	both := `[{"number":1,"met":true,"evidence":"merged"},{"number":2,"met":true,"evidence":"seen"}]`

	if r := review(`[{"number":1,"met":true,"evidence":"merged"}]`); !r.isError || !strings.Contains(r.text, "has no check") {
		t.Fatalf("an unchecked criterion: %+v", r)
	}
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.Waits = &OwnerWait{Reason: "pick a design"}; return true }); err != nil {
		t.Fatal(err)
	}
	if r := review(both); !r.isError || !strings.Contains(r.text, "only the owner can make") || !strings.Contains(r.text, "pick a design") {
		t.Fatalf("the owner's wait: %+v", r)
	}
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.Waits = nil; return true }); err != nil {
		t.Fatal(err)
	}
	deliveryOn(t, s)
	s.setTaskStage(ctx, todo.ID, "review", "https://github.com/o/r/pull/9")
	if r := review(both); !r.isError || !strings.Contains(r.text, "not merged") {
		t.Fatalf("the delivery gate: %+v", r)
	}
	if todoNow(s, todo.ID).DoneAt != nil {
		t.Fatal("closed by a refused review")
	}
	forge.merged, forge.sha, forge.contains = true, mergeSHA, map[string]bool{oldHub: true}
	s.deploy.cache = nil // the refused check is remembered for two minutes
	if r := review(both); r.isError {
		t.Fatalf("the gates pass: %+v", r)
	}
	if got := todoNow(s, todo.ID); got.DoneAt == nil || got.Supervision.state(got) != reviewAccepted {
		t.Fatalf("not closed: %+v", got.Supervision)
	}
}

type acceptedReply struct {
	isError bool
	text    string
}

// hookForge calls onMerge once, as the forge answers its first merge question: what happens to the to-do while a
// review is in the middle of its gates.
type hookForge struct {
	*fakeForge
	once    sync.Once
	onMerge func()
}

func (h *hookForge) PRMerge(ctx context.Context, repo string, number int) (bool, string, error) {
	h.once.Do(h.onMerge)
	return h.fakeForge.PRMerge(ctx, repo, number)
}

// A review that names criteria by number is refused when the list changed while the call ran, and writes nothing. (A
// list that changed between the reviewer's read and the call is not caught: the numbers are read at the call.)
func TestAReviewByNumberIsRefusedWhenTheListChangedDuringTheCall(t *testing.T) {
	forge := &hookForge{fakeForge: mergedAndLive()}
	s, _, todo := typedService(t, forge, true)
	forge.onMerge = func() {
		if _, _, err := s.updateSupervision(context.Background(), todo.ID, func(v *Supervision) bool {
			v.DoneWhen = slices.Clone(slices.Concat([]string{"a new criterion"}, v.DoneWhen))
			return true
		}); err != nil {
			t.Error(err)
		}
	}
	_, err := s.toolTaskReview(context.Background(), "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","verdict":"accepted",
		"checks":[{"number":1,"met":true,"evidence":"merged"},{"number":2,"met":true,"evidence":"seen"}]}`))
	if !errors.Is(err, criteria.ErrChanged) {
		t.Fatalf("stale numbers: %v", err)
	}
	got := todoNow(s, todo.ID)
	if got.DoneAt != nil || len(got.Supervision.Reviews) != 0 {
		t.Fatalf("written: %+v", got.Supervision)
	}
}

// ciForge answers the checks of a pull request's head commit as given.
type ciForge struct {
	*fakeForge
	st  TodoPRStatus
	err error
}

func (f ciForge) PRHeadChecks(context.Context, string, int) (TodoPRStatus, error) { return f.st, f.err }

func TestCIGreenAnswersForAMergedPullRequestFromItsHeadCommit(t *testing.T) {
	for _, tc := range []struct {
		name, state, checks, want, evidence string
	}{
		{"merged, green", "merged", "success", "1. met: ", "head1234"},
		{"merged, a failed check", "merged", "failure", "1. not met: ", "failing checks (commit head1234)"},
		{"closed, not merged", "closed", "none", "1. cannot: ", "closed without a merge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, todo := typedService(t, ciForge{fakeForge: mergedAndLive(), st: TodoPRStatus{State: tc.state, HeadOID: "head1234abcd", Checks: tc.checks}}, true)
			if _, err := declareChecks(s, todo.ID, `[{"criterion":1,"kind":"ci_green"}]`); err != nil {
				t.Fatal(err)
			}
			res, err := s.toolTaskReview(context.Background(), "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","verify":true}`))
			if err != nil || !strings.Contains(res, tc.want) || !strings.Contains(res, tc.evidence) {
				t.Fatalf("%q %v", res, err)
			}
		})
	}
}

// slowForge holds the first merge question until gate closes: a hub run that is in flight.
func slowForge(inner *fakeForge) (*hookForge, chan struct{}) {
	gate := make(chan struct{})
	return &hookForge{fakeForge: inner, onMerge: func() { <-gate }}, gate
}

func quickDigest(s *Service, hub time.Duration) {
	s.superviseTimes = superviseTimings{gather: time.Millisecond, every: time.Millisecond, urgent: time.Millisecond, hub: hub}
}

func reviewSent(f *fakeHub) int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sends) }

func TestAReviewWaitsForTheHubsRunAndIsDroppedWhenTheHubAccepted(t *testing.T) {
	forge, gate := slowForge(mergedAndLive())
	s, f, todo := typedService(t, forge, true)
	quickDigest(s, time.Minute)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.sendDigest(context.Background())
	if reviewSent(f) != 0 {
		t.Fatalf("the review went out while the hub's run was in flight: %v", f.sends)
	}
	close(gate)
	s.tasks.outcomeRuns.Wait()
	s.sendDigest(context.Background())
	if got := todoNow(s, todo.ID); got.DoneAt == nil || reviewSent(f) != 0 {
		t.Fatalf("accepted by the hub, closed %v, sends %v", got.DoneAt, f.sends)
	}
}

func TestAReviewIsDeliveredWithTheHubsEvidenceWhenACheckFailed(t *testing.T) {
	forge, gate := slowForge(&fakeForge{merged: true, sha: mergeSHA, contains: map[string]bool{oldHub: false}})
	s, f, todo := typedService(t, forge, true)
	quickDigest(s, time.Minute)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.sendDigest(context.Background())
	if reviewSent(f) != 0 {
		t.Fatalf("delivered early: %v", f.sends)
	}
	close(gate)
	s.tasks.outcomeRuns.Wait()
	waitFor(t, "the review", func() bool { s.sendDigest(context.Background()); return reviewSent(f) > 0 })
	if c := todoNow(s, todo.ID).Supervision.Criteria[1]; c.By != "hub" || c.State != "not_met" {
		t.Fatalf("no hub evidence on the ledger: %+v", c)
	}
}

func TestAReviewGoesOutAfterTheBoundWhenTheHubsRunHangs(t *testing.T) {
	forge, gate := slowForge(mergedAndLive())
	t.Cleanup(func() { close(gate) })
	s, f, todo := typedService(t, forge, true)
	quickDigest(s, 1500*time.Millisecond) // the digest ignores a hold shorter than a second
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID)
	s.sendDigest(context.Background())
	if reviewSent(f) != 0 {
		t.Fatalf("delivered before the bound: %v", f.sends)
	}
	waitFor(t, "the review after the bound", func() bool { s.sendDigest(context.Background()); return reviewSent(f) > 0 })
}

func TestTheHubsAcceptanceAndAReviewersOwnTakeEffectOnce(t *testing.T) {
	forge, gate := slowForge(mergedAndLive())
	s, _, todo := typedService(t, forge, true)
	if _, err := declareChecks(s, todo.ID, bothTyped); err != nil {
		t.Fatal(err)
	}
	reviewWaits(t, s, todo.ID) // the hub's run starts and waits at the forge
	var wg sync.WaitGroup
	var reviewerErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, reviewerErr = acceptReview(s, todo.ID, nil)
	}()
	close(gate)
	wg.Wait()
	s.tasks.outcomeRuns.Wait()
	v := ledgerOf(s, todo.ID)
	accepted := 0
	for _, r := range v.Reviews {
		if r.Verdict == reviewAccepted {
			accepted++
		}
	}
	if got := todoNow(s, todo.ID); got.DoneAt == nil || accepted != 1 {
		t.Fatalf("closed %v, accepted reviews %d: %+v", got.DoneAt, accepted, v.Reviews)
	}
	if reviewerErr != nil && !strings.Contains(reviewerErr.Error(), "already") {
		t.Fatalf("the loser was told: %v", reviewerErr)
	}
}
