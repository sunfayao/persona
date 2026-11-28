package prompts

import (
	"strings"
	"testing"
)

// A joining member is told the to-do's goal, its own part and the criteria it answers for by their numbers, and that the
// others' work is theirs.
func TestTaskMemberNamesItsPartAndItsCriteriaByNumber(t *testing.T) {
	t.Parallel()
	got := RenderTaskMember(TaskMember{ID: "todo_a", Title: "Fix login", Goal: "login works on both stacks", Part: "the web side", Expect: 30,
		Criteria: []MemberCriterion{{Number: 2, Text: "screenshots in the PR"}, {Number: 3, Text: "tests pass"}}})
	for _, want := range []string{`to-do todo_a "Fix login"`, "Goal: login works on both stacks", "Your part: the web side", "  2. screenshots in the PR",
		"  3. tests pass", "about 30 minutes", "one acceptance", "do not stop, redo or undo"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	bare := RenderTaskMember(TaskMember{ID: "todo_a", Title: "Fix login", Goal: "g", Part: "p"})
	if !strings.Contains(bare, "no criterion of your own") || strings.Contains(bare, "Expected:") {
		t.Errorf("a member with no criteria or estimate:\n%s", bare)
	}
}
