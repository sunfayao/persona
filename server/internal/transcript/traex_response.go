package transcript

import (
	"encoding/json"
	"fmt"

	"github.com/sunfayao/persona/server/internal/proto"
)

// RolloutToolDecoder supplies function-call metadata missing from a rollout's display items.
type RolloutToolDecoder interface {
	Call(json.RawMessage) (RolloutToolCall, bool)
	Result(json.RawMessage) (RolloutToolResult, bool)
	PlanOutput(name, output string) string
}

type RolloutToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type RolloutToolResult struct {
	ID      string `json:"call_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

func parseTraexResponse(seq int, line codexLine, meta *Meta) []proto.Item {
	decoder := meta.RolloutTools
	if decoder == nil {
		return nil
	}
	if call, ok := decoder.Call(line.Payload); ok {
		if meta.traexCalls == nil {
			meta.traexCalls = make(map[string]RolloutToolCall)
		}
		meta.traexCalls[call.ID] = call
		return nil
	}
	if len(meta.traexCalls) == 0 {
		return nil
	}
	result, ok := decoder.Result(line.Payload)
	if !ok {
		return nil
	}
	call, ok := meta.traexCalls[result.ID]
	if !ok {
		return nil
	}
	// File and search results already have display records. The checklist calls have only the function response.
	if !planTools[call.Name] || meta.codexSeen[result.ID] {
		return nil
	}
	delete(meta.traexCalls, result.ID)
	mk := func(index int, id, role, kind string) proto.Item {
		return proto.Item{ID: fmt.Sprintf("%s#%d", id, index), Seq: seq, TS: line.Timestamp, Role: role, Kind: kind}
	}
	items := codexTool(meta, mk, result.ID, call.Name, call.Input, decoder.PlanOutput(call.Name, result.Output), result.IsError)
	meta.codexSeenAdd(result.ID)
	meta.codexWorked(items)
	for _, item := range items {
		meta.observeActivity(item)
	}
	return meta.codexModel(items)
}
