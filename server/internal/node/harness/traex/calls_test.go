package traex

import "testing"

func TestPlanOutputKeepsCreationIDAndReceipt(t *testing.T) {
	t.Parallel()
	tools := RolloutTools{}
	const receipt = "Task 4 created (status: pending)."
	if got := tools.PlanOutput("TaskCreate", receipt); got != `{"id":"4","output":"Task 4 created (status: pending)."}` {
		t.Fatalf("creation output = %s", got)
	}
	if got := tools.PlanOutput("TaskUpdate", receipt); got != receipt {
		t.Fatalf("update receipt = %s", got)
	}
}
