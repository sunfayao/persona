// Package turnmodel classifies canonical tool names and projects facets onto session state.
package turnmodel

import (
	"maps"
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

//go:generate go run ./genregistry -out ../../../web/src/lib/tool-registry-wire.ts
//go:generate go run ./genregistry -out ../../../integrations/runtime-sdk/lib/tool-registry.js

var canonicalKinds = map[string]string{
	"Bash":            proto.ToolKindExecute,
	"Read":            proto.ToolKindRead,
	"Edit":            proto.ToolKindEdit,
	"MultiEdit":       proto.ToolKindEdit,
	"NotebookEdit":    proto.ToolKindEdit,
	"ApplyPatch":      proto.ToolKindEdit,
	"Write":           proto.ToolKindWrite,
	"Grep":            proto.ToolKindSearch,
	"Glob":            proto.ToolKindSearch,
	"LS":              proto.ToolKindSearch,
	"WebFetch":        proto.ToolKindFetch,
	"WebSearch":       proto.ToolKindWebSearch,
	"TodoWrite":       proto.ToolKindPlan,
	"TaskCreate":      proto.ToolKindPlan,
	"TaskUpdate":      proto.ToolKindPlan,
	"TaskList":        proto.ToolKindPlan,
	"TaskGet":         proto.ToolKindPlan,
	"AskUserQuestion": proto.ToolKindAsk,
	"ExitPlanMode":    proto.ToolKindMode,
	"EnterPlanMode":   proto.ToolKindMode,
	"Agent":           proto.ToolKindAgent,
	"Task":            proto.ToolKindAgent,
	"SendMessage":     proto.ToolKindMessage,
	"Wait":            proto.ToolKindMessage,
	"TaskOutput":      proto.ToolKindTask,
	"TaskStop":        proto.ToolKindTask,
	"Schedule":        proto.ToolKindSchedule,
	"ScheduleWakeup":  proto.ToolKindSchedule,
	"CronCreate":      proto.ToolKindSchedule,
	"CronDelete":      proto.ToolKindSchedule,
	"Workflow":        proto.ToolKindWorkflow,
	"Artifact":        proto.ToolKindArtifact,
	"ViewImage":       proto.ToolKindImage,
	"GenerateImage":   proto.ToolKindImage,
	"Skill":           proto.ToolKindSkill,
}

// CanonicalRegistry returns the names and kinds shared by the node and browser.
func CanonicalRegistry() map[string]string { return maps.Clone(canonicalKinds) }

// KindOf returns the canonical kind for an exact tool name, or other when it is not recognized.
func KindOf(toolName string) string {
	if kind, ok := canonicalKinds[toolName]; ok {
		return kind
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
