package assistant

import (
	"context"
	"testing"
	"time"
)

// #11553: a plan's task that takes a to-do over (task_plan continues, or its todo_id) starts a new round of the
// to-do's supervision. The earlier round's terminal review stays in the ledger's history, but the ledger no longer
// says accepted or cancelled: no client shows the earlier acceptance on the new work, the to-do is not filed under
// Just finished or Done when its new Agent stops, and the new round's stop short reaches its supervisor. Before, the
// continued to-do sat in Working with the earlier round's green Accepted chip, and superviseJudged dropped its stops
// as more work sent after an acceptance.

// A plan's todo_id takeover of an open to-do whose acceptance stood (its Agent was sent more after the acceptance,
// #3156, and then stopped) is a new round too.
func TestTodoIDTakeoverStartsANewSupervisionRound(t *testing.T) {
	e := newThreadEnv(t, nil)
	e.stoppedAgent01()
	accepted := TaskReview{At: time.Now().Add(-time.Hour), Verdict: reviewAccepted, Note: "Released 1.2"}
	putTodo(e.s, Todo{ID: "todo_x", Title: "发版", Origin: "m1",
		Supervision: &Supervision{Supervisor: "m1", State: reviewAccepted, Reviews: []TaskReview{accepted}},
		Agents:      []TodoAgent{{MachineID: "box", SessionID: "agent01"}}})
	if !acceptanceStands(todoNow(e.s, "todo_x")) {
		t.Fatal("the reopened to-do's acceptance does not stand to begin with")
	}
	if _, err := e.s.toolTaskPlan(context.Background(), "m1", reuseArgs(true, "todo_x")); err != nil {
		t.Fatal(err)
	}
	newAgentOn(t, e, "todo_x")
	got := todoNow(e.s, "todo_x")
	if got.Supervision.State != "" || acceptanceStands(got) {
		t.Fatalf("the takeover's new Agent works under the earlier acceptance: %+v", got.Supervision)
	}
	if len(got.Supervision.Reviews) != 1 || got.Supervision.Reviews[0].Verdict != reviewAccepted {
		t.Fatalf("the earlier acceptance left the history: %+v", got.Supervision.Reviews)
	}
}

// The rule stays for what it was made for: more work sent to an accepted task's Agent reopens it while its acceptance
// stands (#3156, #7539), and so does a Reopen by hand; neither is a plan's new Agent.
func TestMoreWorkForAnAcceptedTaskKeepsItsAcceptance(t *testing.T) {
	s, f, accepted := acceptedPlanTask(t, nil)
	if err := sendTo(t, s, "agent_send", "conv1", "Also attach the dark screenshot"); err != nil {
		t.Fatal(err)
	}
	longTurn(s, f, "working", "")
	waitFor(t, "the reopen", func() bool { return todoNow(s, accepted.ID).DoneAt == nil })
	got := todoNow(s, accepted.ID)
	if got.Supervision.State != reviewAccepted || !acceptanceStands(got) {
		t.Fatalf("more work for the accepted task dropped its acceptance: %+v", got.Supervision)
	}
}
