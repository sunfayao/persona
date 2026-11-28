package prompts

import (
	"os"
	"strings"
	"testing"
)

// The note of a supervisor Agent the hub attached lists what it supervises with its numbered criteria, and stays short:
// every such Agent carries it as its first message.
func TestTaskSupervisorAttachedListsWhatItSupervises(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("agents/task_supervisor_attached.md")
	if err != nil || len(raw) >= 1600 {
		t.Fatalf("template: %d bytes, %v", len(raw), err)
	}
	got := RenderTaskSupervisorAttached(TaskSupervisorAttached{Todos: []SupervisedTodo{
		{ID: "todo_a", Title: "Fix login", Machine: "box", Criteria: []string{"PR merged", "tests pass"}},
		{ID: "todo_b", Title: "Docs"},
	}})
	for _, want := range []string{`- todo_a "Fix login" on box`, "  1. PR merged", "  2. tests pass", `- todo_b "Docs"`, "task_review", "agent_read", "report_to_supervisor"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"Docs" on`) || strings.Contains(got, "hand its parts to workers") {
		t.Errorf("wrong text:\n%s", got)
	}
}
