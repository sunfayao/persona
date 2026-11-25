package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// relayedPlan is task_plan's arguments for one task on box the model marks user_requested: writable unless over says
// otherwise.
func relayedPlan(over map[string]any) string {
	task := map[string]any{"title": "Fix the export", "prompt": "The export drops the old format: fix it and add a test",
		"role": "worker", "machine": "box", "cwd": "/code/app", "readonly": false, "permission": "bypassPermissions", "user_requested": true,
		"why": "the repository is on box", "done_when": []string{"the export keeps the old format"}, "expect_minutes": 30}
	for k, v := range over {
		task[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"tasks": []any{task}})
	return string(raw)
}

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

// taskReport is the item a task's report leaves in a conversation: Persona's own input, not the owner's.
func taskReport() proto.Item {
	return proto.Item{ID: "n1", Seq: 3, Role: "user", Kind: proto.KindText, Text: "🔔 task done: the export audit",
		Source: &proto.MessageSource{Kind: proto.SourceSupervision}}
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

// newStartRuleHarness is the dispatch harness on st, a database that keeps and starts task plans (nil: a new one),
// with the computer box for tasks.
func newStartRuleHarness(t *testing.T, st Store) *dispatchPolicyHarness {
	t.Helper()
	if st == nil {
		st = planUpdateStore{&planMemStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}}}
	}
	d := newDispatchPolicyHarnessOn(t, st)
	d.f.setSessions("box")
	return d
}
