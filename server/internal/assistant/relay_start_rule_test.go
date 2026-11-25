package assistant

import (
	"context"
	"testing"
	"time"
)

// startsSoFar counts the agents started so far.
func (d *dispatchPolicyHarness) startsSoFar() int {
	d.f.mu.Lock()
	defer d.f.mu.Unlock()
	return len(d.f.starts)
}

// handOver hands the owner's quoted words from the main conversation to the lead, which takes them in at once; the
// lead's transcript then ends with them.
func (d *dispatchPolicyHarness) handOver(t *testing.T) {
	t.Helper()
	d.f.setItems(ownerItem("m-owner", quotedWords))
	d.send(t, d.mainID, d.leaderID, `"text":"please fix the export","quote":"latest"`)
	d.f.setItems(received(t, d.f, d.mainID))
}

// startedPlanOf keeps a started plan of conversation conv with one writable task in cwd on box: a kind the owner
// let that conversation start.
func (d *dispatchPolicyHarness) startedPlanOf(t *testing.T, conv, cwd string) {
	t.Helper()
	now := time.Now().UTC()
	p := &TaskPlan{ID: "plan_" + randomHex(6), Conversation: conv, State: planStarted, CreatedAt: now, UpdatedAt: now,
		Tasks: []PlanTask{{Title: "Earlier work", Prompt: "earlier", Machine: "box", Cwd: cwd}}}
	if err := d.s.savePlan(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

// planIn plans relayedPlan(over) in conversation id and says whether it started at once (the agent started) or waits
// for Start (none did).
func (d *dispatchPolicyHarness) planIn(t *testing.T, id string, over map[string]any) bool {
	t.Helper()
	before := d.startsSoFar()
	res := d.tool(t, id, "task_plan", relayedPlan(over))
	out := res.Content[0].Text
	if res.IsError {
		t.Fatalf("task_plan: %s", out)
	}
	if waitsForStart(out) {
		waitFor(t, "the start reservation to settle", func() bool { return d.s.taskStarting.Load() == 0 })
		if n := d.startsSoFar() - before; n != 0 {
			t.Fatalf("%d agents started on a plan that waits", n)
		}
		return false
	}
	waitFor(t, "the agent to start", func() bool { return d.startsSoFar() == before+1 })
	return true
}
