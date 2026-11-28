package transcript

import (
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestDecodeRolloutCallUsesNativeArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		record string
		name   string
		input  string
	}{
		{`{"type":"function_call","call_id":"native-id","name":"Read","arguments":"{\"file_path\":\"example.txt\"}"}`, "Read", `{"file_path":"example.txt"}`},
		{`{"type":"function_call","call_id":"native-id","name":"Grep","arguments":{"pattern":"needle"}}`, "Grep", `{"pattern":"needle"}`},
		{`{"type":"function_call_output","call_id":"native-id","name":"Grep","arguments":{}}`, "", ""},
		{`{"type":"function_call","call_id":"native-id","name":"Read","arguments":"not JSON"}`, "", ""},
		{`{"type":"function_call","name":"Read","arguments":{}}`, "", ""},
	} {
		var record rolloutToolRecord
		if err := json.Unmarshal([]byte(tc.record), &record); err != nil {
			t.Fatal(err)
		}
		call, ok := record.decodeRolloutCall(record.callInput())
		if ok != (tc.name != "") || call.Name != tc.name || string(call.Input) != tc.input || ok && call.ID != "native-id" {
			t.Errorf("decodeRolloutCall(%s) = %+v, %t", tc.record, call, ok)
		}
	}
}

func TestCodexPlanUpdatesChecklistAndClearsCompletedPlan(t *testing.T) {
	t.Parallel()
	var meta Meta
	const call = `{"timestamp":"2026-01-01T00:00:00Z","type":"response_item","payload":{"type":"function_call","call_id":"plan-1","name":"update_plan","arguments":{"plan":[{"step":"Check input","status":"completed"},{"step":"Run the suite","status":"in_progress"}]}}}`
	const result = `{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"plan-1","output":"Plan updated"}}`
	if items := ParseLine(1, []byte(call), &meta); len(items) != 0 {
		t.Fatalf("plan call emitted twice: %+v", items)
	}
	items := ParseLine(2, []byte(result), &meta)
	if len(items) != 2 || items[0].Kind != proto.KindToolUse || items[0].ToolName != "TodoWrite" || items[0].ToolUseID != "plan-1" {
		t.Fatalf("plan items = %+v", items)
	}
	if got := meta.Plan; got == nil || got.Total != 2 || got.Done != 1 || got.Current != "Run the suite" {
		t.Fatalf("plan = %+v", got)
	}

	const clear = `{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"plan-2","name":"update_plan","arguments":{"plan":[]}}}`
	const cleared = `{"timestamp":"2026-01-01T00:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"plan-2","output":"Plan updated"}}`
	ParseLine(3, []byte(clear), &meta)
	if items := ParseLine(4, []byte(cleared), &meta); len(items) != 2 || meta.Plan != nil {
		t.Fatalf("clear plan emitted %+v and left %+v", items, meta.Plan)
	}
}
