package turnmodel

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestKindOfClassifiesCanonicalTools(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		kind  string
		names []string
	}{
		{proto.ToolKindExecute, []string{"Bash"}},
		{proto.ToolKindRead, []string{"Read"}},
		{proto.ToolKindEdit, []string{"Edit", "MultiEdit", "NotebookEdit", "ApplyPatch"}},
		{proto.ToolKindWrite, []string{"Write"}},
		{proto.ToolKindSearch, []string{"Grep", "Glob", "LS"}},
		{proto.ToolKindFetch, []string{"WebFetch"}},
		{proto.ToolKindWebSearch, []string{"WebSearch"}},
		{proto.ToolKindPlan, []string{"TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet"}},
		{proto.ToolKindAsk, []string{"AskUserQuestion"}},
		{proto.ToolKindMode, []string{"ExitPlanMode", "EnterPlanMode"}},
		{proto.ToolKindAgent, []string{"Agent", "Task"}},
		{proto.ToolKindMessage, []string{"SendMessage", "Wait"}},
		{proto.ToolKindTask, []string{"TaskOutput", "TaskStop"}},
		{proto.ToolKindSchedule, []string{"Schedule", "ScheduleWakeup", "CronCreate", "CronDelete"}},
		{proto.ToolKindWorkflow, []string{"Workflow"}},
		{proto.ToolKindArtifact, []string{"Artifact"}},
		{proto.ToolKindImage, []string{"ViewImage", "GenerateImage"}},
		{proto.ToolKindMcp, []string{"mcp__workspace__format"}},
		{proto.ToolKindSkill, []string{"Skill"}},
		{proto.ToolKindOther, []string{"", "bash", "Delete", "Move", "mcp__workspace", "mcp____format", "mcp__workspace__"}},
	} {
		t.Run(c.kind, func(t *testing.T) {
			t.Parallel()
			for _, name := range c.names {
				if got := KindOf(name); got != c.kind {
					t.Errorf("KindOf(%q) = %q, want %q", name, got, c.kind)
				}
			}
		})
	}
}

func TestSplitMCPRequiresBothNames(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, server, tool string
	}{
		{"mcp__workspace__format", "workspace", "format"},
		{"mcp__workspace__format__file", "workspace", "format__file"},
		{"", "", ""},
		{"other__workspace__format", "", ""},
		{"mcp__workspace", "", ""},
		{"mcp____format", "", ""},
		{"mcp__workspace__", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			server, tool, ok := SplitMCP(c.name)
			if server != c.server || tool != c.tool || ok != (c.server != "") {
				t.Errorf("SplitMCP(%q) = %q, %q, %v", c.name, server, tool, ok)
			}
		})
	}
}
