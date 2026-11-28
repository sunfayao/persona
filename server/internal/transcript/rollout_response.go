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

func decodeRolloutCall(payload json.RawMessage) (RolloutToolCall, bool) {
	if !bytes.Contains(payload, []byte(`"function_call"`)) {
		return RolloutToolCall{}, false
	}
	var record struct {
		Type      string          `json:"type"`
		ID        string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(payload, &record) != nil || record.Type != "function_call" || record.ID == "" {
		return RolloutToolCall{}, false
	}
	input := record.Arguments
	if len(input) > 0 && input[0] == '"' {
		var arguments string
		if json.Unmarshal(input, &arguments) != nil {
			return RolloutToolCall{}, false
		}
		input = json.RawMessage(arguments)
	}
	input = bytes.TrimSpace(input)
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return RolloutToolCall{}, false
	}
	return RolloutToolCall{ID: record.ID, Name: record.Name, Input: input}, true
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
	decoder := meta.RolloutTools
	if call, ok := decodeRolloutCall(line.Payload); ok {
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
		return nil
	}
	if len(meta.rolloutCalls) == 0 || !bytes.Contains(line.Payload, []byte(`"function_call_output"`)) {
		return nil
	}
	var result struct {
		Type    string `json:"type"`
		ID      string `json:"call_id"`
		Output  string `json:"output"`
		IsError bool   `json:"is_error"`
	}
	if json.Unmarshal(line.Payload, &result) != nil || result.Type != "function_call_output" {
		return nil
	}
	call, ok := meta.rolloutCalls[result.ID]
	if !ok {
		return nil
	}
	// File and search results already have display records. The checklist calls have only the function response.
	if !planTools[call.Name] || meta.codexSeen[result.ID] {
		return nil
	}
	delete(meta.rolloutCalls, result.ID)
	mk := func(index int, id, role, kind string) proto.Item {
		return proto.Item{ID: fmt.Sprintf("%s#%d", id, index), Seq: seq, TS: line.Timestamp, Role: role, Kind: kind}
	}
	if decoder != nil {
		result.Output = decoder.PlanOutput(call.Name, result.Output)
	}
	items := codexTool(meta, mk, result.ID, call.Name, call.Input, result.Output, result.IsError)
	meta.codexSeenAdd(result.ID)
	meta.codexWorked(items)
	for _, item := range items {
		meta.observeActivity(item)
	}
	return meta.codexModel(items)
}
