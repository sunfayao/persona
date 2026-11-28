package traex

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sunfayao/persona/server/internal/transcript"
)

// RolloutTools decodes the function records that accompany TRAE's display items.
type RolloutTools struct{}

// Call reads the response_item that gives an agent command its tool name and input.
func (RolloutTools) Call(payload json.RawMessage) (transcript.RolloutToolCall, bool) {
	if !bytes.Contains(payload, []byte(`"function_call"`)) {
		return transcript.RolloutToolCall{}, false
	}
	var record struct {
		Type      string          `json:"type"`
		ID        string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(payload, &record) != nil || record.Type != "function_call" || record.ID == "" {
		return transcript.RolloutToolCall{}, false
	}
	switch record.Name {
	case "Read", "Grep", "Glob", "Skill", "Edit", "MultiEdit", "Write",
		"TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet", "TaskOutput", "TaskStop":
	default:
		return transcript.RolloutToolCall{}, false
	}
	input := record.Arguments
	if len(input) > 0 && input[0] == '"' {
		var arguments string
		if json.Unmarshal(input, &arguments) != nil {
			return transcript.RolloutToolCall{}, false
		}
		input = json.RawMessage(arguments)
	}
	input = bytes.TrimSpace(input)
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return transcript.RolloutToolCall{}, false
	}
	return transcript.RolloutToolCall{ID: record.ID, Name: record.Name, Input: input}, true
}

// Result reads the matching function_call_output record.
func (RolloutTools) Result(payload json.RawMessage) (transcript.RolloutToolResult, bool) {
	if !bytes.Contains(payload, []byte(`"function_call_output"`)) {
		return transcript.RolloutToolResult{}, false
	}
	var record struct {
		Type string `json:"type"`
		transcript.RolloutToolResult
	}
	if json.Unmarshal(payload, &record) != nil || record.Type != "function_call_output" || record.ID == "" {
		return transcript.RolloutToolResult{}, false
	}
	return record.RolloutToolResult, true
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
