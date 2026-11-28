// Package turnmodel classifies canonical tool names and projects facets onto session state.
package turnmodel

import (
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// KindOf returns the canonical kind for an exact tool name, or other when it is not recognized.
func KindOf(toolName string) string {
	switch toolName {
	case "Bash":
		return proto.ToolKindExecute
	case "Read":
		return proto.ToolKindRead
	case "Edit", "MultiEdit", "NotebookEdit", "ApplyPatch":
		return proto.ToolKindEdit
	case "Write":
		return proto.ToolKindWrite
	case "Grep", "Glob", "LS":
		return proto.ToolKindSearch
	case "WebFetch":
		return proto.ToolKindFetch
	case "WebSearch":
		return proto.ToolKindWebSearch
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet":
		return proto.ToolKindPlan
	case "AskUserQuestion":
		return proto.ToolKindAsk
	case "ExitPlanMode", "EnterPlanMode":
		return proto.ToolKindMode
	case "Agent", "Task":
		return proto.ToolKindAgent
	case "SendMessage", "Wait":
		return proto.ToolKindMessage
	case "TaskOutput", "TaskStop":
		return proto.ToolKindTask
	case "Schedule", "ScheduleWakeup", "CronCreate", "CronDelete":
		return proto.ToolKindSchedule
	case "Workflow":
		return proto.ToolKindWorkflow
	case "Artifact":
		return proto.ToolKindArtifact
	case "ViewImage", "GenerateImage":
		return proto.ToolKindImage
	case "Skill":
		return proto.ToolKindSkill
	}
	if _, _, ok := SplitMCP(toolName); ok {
		return proto.ToolKindMcp
	}
	return proto.ToolKindOther
}

// SplitMCP splits mcp__<server>__<tool> at the first separator after the prefix.
// Both names must be nonempty; the tool name may itself contain double underscores.
func SplitMCP(name string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	if !ok || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}
