package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// deployedForReview has supervised todo's agent say its work is deployed until its "Please verify" card is out and the
// task waits for review. The card goes to the owner as a big feature's does (ownerVerifiesAll), or as one from before
// the assistant accepted deployed work itself (#3205).
func deployedForReview(t *testing.T, s *Service, f *fakeHub, todo string) {
	t.Helper()
	s.ownerVerifiesAll = true
	f.judge = `{"changed":"飞书机器人多了任务进展开关","link":"https://hub.example.com/settings?tab=feishu","how":"打开设置看开关"}`
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the 请验收 card", func() bool { a := askOf(s, todo); return a != nil && a.MessageID != "" })
	f.judge = `{"verdict":"done","reason":"已部署"}`
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitFor(t, "the review", func() bool { return todoNow(s, todo).Supervision.State == reviewPending })
}

// updatedCards keeps the cards the bot redraws.
func updatedCards(f *fakeHub) func() []string {
	var mu sync.Mutex
	var cards []string
	f.answerApp(AppFeishu, FeishuUpdateCard, func(p json.RawMessage) (any, error) {
		var m struct {
			Card string `json:"card"`
		}
		_ = json.Unmarshal(p, &m)
		mu.Lock()
		cards = append(cards, m.Card)
		mu.Unlock()
		return map[string]any{}, nil
	})
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), cards...) }
}

func waitingForOwner(s *Service) int {
	ctx := context.Background()
	_, total := s.snapshotUnaccepted(ctx, s.location(ctx))
	return total
}

// The supervisor's accepted, having checked the evidence, passes the deployed task's "Please verify": the task is
// verified, the card says the supervisor passed it, and nothing waits for the owner's "Pass" (#3240).
func TestSupervisorAcceptedPassesPleaseVerify(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, false)
	updated := updatedCards(f)
	deployedForReview(t, s, f, todo.ID)
	res := reviewTask(t, s, todo.ID, map[string]any{"verdict": "accepted", "note": "上线了",
		"checks": []ReviewCheck{
			{Criterion: "PR 合并到 master", Met: true, Evidence: "https://github.com/o/r/pull/9 merged"},
			{Criterion: "浅色 / 深色截图贴进 PR", Met: true, Evidence: "both screenshots verified"},
		}})
	if !strings.Contains(res, "marked verified by you") {
		t.Fatalf("the review's answer: %s", res)
	}
	a := askOf(s, todo.ID)
	if a == nil || a.AcceptedAt.IsZero() || !a.BySupervisor {
		t.Fatalf("ask %+v", a)
	}
	if n := waitingForOwner(s); n != 0 {
		t.Fatalf("%d waiting for the owner", n)
	}
	if got := todoNow(s, todo.ID); got.Stage != "verify" {
		t.Fatalf("stage %q", got.Stage)
	}
	if n := countAcceptCards(posts()); n != 1 {
		t.Fatalf("%d 请验收 cards", n)
	}
	if n := len(cardsWith(posts(), "结论：")); n != 0 {
		t.Fatalf("%d conclusion cards", n)
	}
	if c := cardsWith(updated(), "督办已核对证据，标为已验证"); len(c) != 1 || strings.Contains(c[0], `"a":"accept"`) {
		t.Fatalf("the card is not redrawn as passed by the supervisor: %q", updated())
	}
	// Reminded the next morning: nothing, it is passed.
	s.accept.mu.Lock()
	s.accept.asks[todo.ID].AskedAt = time.Now().Add(-30 * time.Hour)
	s.accept.mu.Unlock()
	n := len(posts())
	ctx := context.Background()
	now := time.Now().In(s.location(ctx))
	s.remindAcceptance(time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, s.location(ctx)))
	if got := posts()[n:]; len(got) != 0 {
		t.Fatalf("reminded: %q", got)
	}
}

// An ask left waiting by a hub from before #3240 (the supervisor accepted, the owner was still asked) is passed by the
// sweep; so is one whose card went out after the review.
func TestSupervisedBacklogIsPassedBySweep(t *testing.T) {
	s, f, todo, _, _ := conclusionService(t, false)
	deployedForReview(t, s, f, todo.ID)
	reviewTask(t, s, todo.ID, map[string]any{"verdict": "accepted", "note": "上线了", "checks": completeReviewChecks(todoNow(s, todo.ID))})
	s.accept.mu.Lock()
	s.accept.asks[todo.ID].AcceptedAt, s.accept.asks[todo.ID].BySupervisor = time.Time{}, false // as the old hub left it
	s.accept.mu.Unlock()
	if n := waitingForOwner(s); n != 1 {
		t.Fatalf("%d waiting before the sweep", n)
	}
	s.verifySupervisedAsks(context.Background())
	if a := askOf(s, todo.ID); a == nil || a.AcceptedAt.IsZero() || !a.BySupervisor {
		t.Fatalf("ask %+v", a)
	}
	if n := waitingForOwner(s); n != 0 {
		t.Fatalf("%d waiting after the sweep", n)
	}
}

func TestLegacyOwnerCheckReturnsToSupervisorOnStartup(t *testing.T) {
	ctx := t.Context()
	store := todoTestDB(t)
	_, f, todo := acceptedForUserCheckOn(t, store)
	restarted := newServiceOn(t, f, store)
	setSettings(t, restarted, func(v *Settings) { v.Main = "conv1" })
	if err := restarted.ensureTodosLoaded(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := restarted.listedTodosOf(ctx, false, map[string]bool{todo.ID: true})
	if err != nil || len(list) != 1 || list[0].Supervision.State != reviewPending || list[0].Ask != nil {
		t.Fatalf("legacy check leaked into the owner projection: %+v, %v", list, err)
	}
	restarted.verifySupervisedAsks(ctx)
	restarted.verifySupervisedAsks(ctx)
	got := todoNow(restarted, todo.ID)
	if got.DoneAt != nil || got.Supervision.State != reviewPending {
		t.Fatalf("legacy check did not return to supervisor review: %+v", got)
	}
	if ask := askOf(restarted, todo.ID); ask == nil || !ask.Superseded || ask.AcceptedAt.IsZero() {
		t.Fatalf("legacy verification was not withdrawn: %+v", ask)
	}
	if sig := watchSignals(got, nil, time.Now()); len(sig) != 1 || sig[0].Kind != sigReview {
		t.Fatalf("the supervisor will not receive the old task: %+v", sig)
	}
	reviewTask(t, restarted, todo.ID, map[string]any{
		"verdict": "accepted", "outcome": "done", "checks": completeReviewChecks(got),
	})
	if row, err := store.GetTodo(ctx, todo.ID); err != nil || row.DoneAt == nil {
		t.Fatalf("supervisor did not close the stored task: %+v, %v", row, err)
	}
	if n := waitingForOwner(restarted); n != 0 {
		t.Fatalf("supervisor acceptance left %d owner waits", n)
	}
}

// A supervisor's checked acceptance closes a high-priority task and settles its previous deployment ask.
func TestSupervisorAcceptsBigFeatureWithoutOwnerPass(t *testing.T) {
	s, f, todo, _, _ := conclusionService(t, false)
	s.todos.mu.Lock()
	s.todos.todos[todo.ID].Priority = "high"
	s.todos.mu.Unlock()
	deployedForReview(t, s, f, todo.ID)
	reviewTask(t, s, todo.ID, map[string]any{
		"verdict": "accepted", "outcome": "done", "checks": completeReviewChecks(todoNow(s, todo.ID)),
	})
	if got := todoNow(s, todo.ID); got.DoneAt == nil || waitingForOwner(s) != 0 {
		t.Fatalf("verified task still awaits the owner: %+v", got)
	}
	if ask := askOf(s, todo.ID); ask == nil || ask.AcceptedAt.IsZero() || !ask.BySupervisor {
		t.Fatalf("the supervisor's verification did not pass the prior ask: %+v", ask)
	}
}

// A task with no supervisor keeps asking the owner: closed by the auto judge, its "Please verify" waits for their "Pass".
func TestUnsupervisedTaskStillAsksTheOwner(t *testing.T) {
	s, f, posts := acceptService(t) // its deployed work asks the owner (ownerVerifiesAll), as a big feature's does
	todo := startedTask(t, s, f)
	if todo.Supervision != nil && todo.Supervision.Supervisor != "" {
		t.Fatalf("the task has a supervisor: %+v", todo.Supervision)
	}
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, deployedReply, 0)
	waitAcceptCard(t, s, todo.ID, posts)
	ctx := context.Background()
	if ok, err := s.autoDoneTodo(ctx, todo.ID, "做完了", time.Now().Add(-time.Minute)); err != nil || !ok {
		t.Fatalf("auto done: %v %v", ok, err)
	}
	s.verifySupervisedAsks(ctx)
	if a := askOf(s, todo.ID); a == nil || !a.AcceptedAt.IsZero() || a.BySupervisor {
		t.Fatalf("ask %+v", a)
	}
	if n := waitingForOwner(s); n != 1 {
		t.Fatalf("%d waiting for the owner", n)
	}
	if n := countAcceptCards(posts()); n != 1 {
		t.Fatalf("%d 请验收 cards", n)
	}
}

// acceptedAfterJudgeDelivery has the judge deliver a checked live result, proves it did not close the task, then has
// main accept it with a completed outcome.
func acceptedAfterJudgeDelivery(t *testing.T, s *Service, f *fakeHub, todo string) {
	t.Helper()
	sampleAt(t, s, 0)
	// The judge says it is live; the reply itself does not, so no deployed "Please verify" stands in for the conclusion.
	f.mu.Lock()
	f.judge = judgeWithChecks("live", true, true)
	f.mu.Unlock()
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "做完了，两条都核对过，结论见上面的报告", 0)
	waitFor(t, "delivered for review", func() bool { return todoNow(s, todo).Supervision.State == reviewPending })
	if todoNow(s, todo).DoneAt != nil {
		t.Fatal("the judge closed the task")
	}
	reviewTask(t, s, todo, map[string]any{"verdict": "accepted", "outcome": "done", "note": "验收通过",
		"checks": completeReviewChecks(todoNow(s, todo))})
	waitFor(t, "closed by main", func() bool { return todoNow(s, todo).DoneAt != nil })
	waitFor(t, "its ask", func() bool { a := askOf(s, todo); return a != nil && !a.AskedAt.IsZero() })
}

// Main's acceptance after a judge delivery passes the owner's conclusion: nothing waits for them (#5732).
func TestAcceptedAfterJudgeDeliveryWaitsForNobody(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, false)
	acceptedAfterJudgeDelivery(t, s, f, todo.ID)
	a := askOf(s, todo.ID)
	if !a.Conclusion || a.AcceptedAt.IsZero() || !a.BySupervisor {
		t.Fatalf("ask %+v", a)
	}
	if n := waitingForOwner(s); n != 0 {
		t.Fatalf("%d waiting for the owner", n)
	}
	if ask := listedAsk(t, s, todo.ID); ask != nil {
		t.Fatalf("still on the to-do: %v", ask)
	}
	for _, c := range posts() {
		if strings.Contains(c, `"a":"accept"`) {
			t.Fatalf("a card still asks the owner:\n%s", c)
		}
	}
	s.verifySupervisedAsks(context.Background())
	if n := waitingForOwner(s); n != 0 {
		t.Fatalf("%d waiting after the sweep", n)
	}
}
