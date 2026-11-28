package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sunfayao/persona/server/internal/proto"
)

// RolloutToolDecoder supplies function-call metadata missing from a rollout's display items.
type RolloutToolDecoder interface {
	Supports(name string) bool
	PlanOutput(name, output string) string
}

type RolloutToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type rolloutToolRecord struct {
	Type      string          `json:"type"`
	ID        string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	IsError   bool            `json:"is_error"`
}

func (r rolloutToolRecord) callInput() json.RawMessage {
	input := r.Arguments
	if r.Type == "custom_tool_call" {
		input = r.Input
	}
	if len(input) > 0 && input[0] == '"' {
		var text string
		if json.Unmarshal(input, &text) == nil && json.Valid([]byte(text)) {
			return json.RawMessage(text)
		}
	}
	return input
}

// decodeRolloutCall accepts the structured arguments that can supply display
// details missing from completed items.
func (r rolloutToolRecord) decodeRolloutCall(input json.RawMessage) (RolloutToolCall, bool) {
	input = bytes.TrimSpace(input)
	if r.Type != "function_call" || r.ID == "" || len(input) == 0 || input[0] != '{' {
		return RolloutToolCall{}, false
	}
	return RolloutToolCall{ID: r.ID, Name: r.Name, Input: input}, true
}

// projectPlan converts a native update_plan checklist to the legacy TodoWrite fields.
func projectPlan(call RolloutToolCall) (RolloutToolCall, bool) {
	var native struct {
		Plan []struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(call.Input, &native); err != nil || native.Plan == nil {
		return RolloutToolCall{}, false
	}
	type todo struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	todos := make([]todo, 0, len(native.Plan))
	for _, step := range native.Plan {
		status := step.Status
		if status == "inProgress" {
			status = "in_progress"
		}
		todos = append(todos, todo{Content: step.Step, Status: status})
	}
	call.Name = "TodoWrite"
	call.Input, _ = json.Marshal(struct {
		Todos []todo `json:"todos"`
	}{Todos: todos})
	return call, true
}

func parseRolloutResponse(seq int, line codexLine, meta *Meta) []proto.Item {
	if !bytes.Contains(line.Payload, []byte(`"function_call"`)) &&
		!(len(meta.rolloutCalls) > 0 && bytes.Contains(line.Payload, []byte(`"function_call_output"`))) &&
		!(meta.toolLoop != nil && (bytes.Contains(line.Payload, []byte(`"function_call_output"`)) ||
			bytes.Contains(line.Payload, []byte(`"custom_tool_call"`)) || bytes.Contains(line.Payload, []byte(`"custom_tool_call_output"`)))) {
		return nil
	}
	var record rolloutToolRecord
	if json.Unmarshal(line.Payload, &record) != nil {
		return nil
	}
	decoder := meta.RolloutTools
	switch record.Type {
	case "function_call", "custom_tool_call":
		input := record.callInput()
		meta.observeToolLoop(&proto.Item{Kind: proto.KindToolUse, TS: line.Timestamp, ToolUseID: record.ID, ToolName: record.Name}, input)
		if call, ok := record.decodeRolloutCall(input); ok {
			if call.Name == "update_plan" {
				call, ok = projectPlan(call)
			} else {
				ok = decoder != nil && decoder.Supports(call.Name)
			}
			if ok {
				if meta.rolloutCalls == nil {
					meta.rolloutCalls = make(map[string]RolloutToolCall)
				}
				meta.rolloutCalls[call.ID] = call
			}
		}
		return nil
	case "function_call_output", "custom_tool_call_output":
		// The response pair has the full payload and both times. The display
		// completion may describe the same call, so never count it again.
		meta.observeToolLoop(&proto.Item{Kind: proto.KindToolResult, TS: line.Timestamp, ToolUseID: record.ID, IsError: record.IsError}, record.Output)
	default:
		return nil
	}
	if record.Type != "function_call_output" {
		return nil
	}
	call, ok := meta.rolloutCalls[record.ID]
	if !ok {
		return nil
	}
	// File and search results already have display records. The checklist calls have only the function response.
	if !planTools[call.Name] || meta.codexSeen[record.ID] {
		return nil
	}
	var output string
	if len(record.Output) != 0 && json.Unmarshal(record.Output, &output) != nil {
		return nil
	}
	delete(meta.rolloutCalls, record.ID)
	mk := func(index int, id, role, kind string) proto.Item {
		return proto.Item{ID: fmt.Sprintf("%s#%d", id, index), Seq: seq, TS: line.Timestamp, Role: role, Kind: kind}
	}
	if decoder != nil {
		output = decoder.PlanOutput(call.Name, output)
	}
	items := codexTool(meta, mk, record.ID, call.Name, call.Input, output, record.IsError)
	meta.codexSeenAdd(record.ID)
	meta.codexWorked(items)
	for _, item := range items {
		meta.observeActivity(item)
	}
	return meta.codexModel(items)
}
