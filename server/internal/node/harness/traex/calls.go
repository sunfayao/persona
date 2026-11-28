package traex

import (
	"encoding/json"
	"strconv"
	"strings"
)

// RolloutTools decodes the function records that accompany TRAE's display items.
type RolloutTools struct{}

// Supports reports whether a tool's native arguments are needed for its display item.
func (RolloutTools) Supports(name string) bool {
	switch name {
	case "Read", "Grep", "Glob", "Skill", "Edit", "MultiEdit", "Write",
		"TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TaskOutput", "TaskStop":
		return true
	}
	return false
}

// PlanOutput projects a creation receipt to the structured task output used by the legacy plan reader.
// TRAE writes "Task 4 created (status: pending)." followed by guidance, with no separate task-id field.
func (RolloutTools) PlanOutput(name, output string) string {
	if name != "TaskCreate" {
		return output
	}
	text, ok := strings.CutPrefix(strings.TrimSpace(output), "Task ")
	head, _, found := strings.Cut(text, " (status: ")
	if !ok || !found {
		return output
	}
	id, _, _ := strings.Cut(head, " ")
	if n, err := strconv.ParseUint(id, 10, 64); err != nil || n == 0 {
		return output
	}
	data, _ := json.Marshal(map[string]string{"id": id, "output": output})
	return string(data)
}
