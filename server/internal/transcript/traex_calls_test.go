package transcript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/node/harness/traex"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

func TestTraexAgentCommandsKeepToolInputAndOutcome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"Read", `{"file_path":"/work/example.txt","offset":2,"limit":3}`},
		{"Grep", `{"pattern":"needle","path":"/work"}`},
		{"Glob", `{"pattern":"**/*.txt"}`},
		{"Skill", `{"name":"review"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := transcript.Meta{RolloutTools: traex.RolloutTools{}}
			feedTraexCall(t, &meta, 1, "call-1", tc.name, tc.input)
			items := feedTraexCommand(t, &meta, 2, "call-1", "agent", "output")
			if len(items) != 2 || items[0].Kind != proto.KindToolUse || items[1].Kind != proto.KindToolResult ||
				items[0].ToolName != tc.name || items[0].ToolUseID != "call-1" ||
				string(items[0].ToolInput) != tc.input || items[1].Text != "output" || items[1].ToolUseID != "call-1" {
				t.Fatalf("tool projection = %+v", items)
			}
			outcome := items[1].Outcome
			if outcome == nil || outcome.ExitCode == nil || *outcome.ExitCode != 0 ||
				outcome.DurationMS == nil || *outcome.DurationMS != 125 {
				t.Fatalf("tool outcome = %+v", outcome)
			}
		})
	}
}

func TestTraexAgentCommandsTrackTaskChanges(t *testing.T) {
	t.Parallel()
	meta := transcript.Meta{RolloutTools: traex.RolloutTools{}}
	feedTraexCall(t, &meta, 1, "create", "TaskCreate", `{"subject":"Review the change","activeForm":"Reviewing the change"}`)
	items := feedTraexResult(t, &meta, 2, "create", "Task 1 created (status: pending).")
	if len(items) != 2 || items[0].ToolName != "TaskCreate" || items[1].ToolUseID != "create" {
		t.Fatalf("create items = %+v", items)
	}
	feedTraexCall(t, &meta, 3, "update", "TaskUpdate", `{"taskId":"1","status":"in_progress"}`)
	feedTraexResult(t, &meta, 4, "update", "Task 1 updated (status: in_progress).")
	if got := meta.Plan; got == nil || got.Total != 1 || got.Done != 0 || got.Current != "Reviewing the change" {
		t.Fatalf("active plan = %+v", got)
	}
	feedTraexCall(t, &meta, 5, "complete", "TaskUpdate", `{"taskId":"1","status":"completed"}`)
	feedTraexResult(t, &meta, 6, "complete", "Task 1 updated (status: completed).")
	if got := meta.Plan; got == nil || got.Total != 1 || got.Done != 1 || got.Current != "" {
		t.Fatalf("completed plan = %+v", got)
	}
}

func TestTraexCallDoesNotRenameShellCommands(t *testing.T) {
	t.Parallel()
	meta := transcript.Meta{RolloutTools: traex.RolloutTools{}}
	feedTraexCall(t, &meta, 1, "call-1", "Read", `{"file_path":"/work/example.txt"}`)
	items := feedTraexCommand(t, &meta, 2, "call-1", "unified_exec_startup", "shell output")
	if len(items) != 2 || items[0].ToolName != "Bash" {
		t.Fatalf("shell command = %+v", items)
	}
}

func TestTraexRolloutJoinsCallsAcrossRefreshes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	const call = `{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"read-1","name":"Read","arguments":{"file_path":"example.txt"}}}` + "\n"
	const result = `{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"CommandExecution","id":"read-1","source":"agent","command":["Read"],"status":"completed","aggregated_output":"A line of text"}}}` + "\n"
	if err := os.WriteFile(path, []byte(call), 0o600); err != nil {
		t.Fatal(err)
	}
	f := transcript.NewFileWithRolloutTools(path, traex.RolloutTools{})
	if items, err := f.Refresh(); err != nil || len(items) != 0 {
		t.Fatalf("call refresh = %+v, %v", items, err)
	}
	out, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.WriteString(result); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if items, err := f.Refresh(); err != nil || len(items) != 2 || items[0].ToolName != "Read" || items[1].Text != "A line of text" {
		t.Fatalf("result refresh = %+v, %v", items, err)
	}
}

func feedTraexCall(t *testing.T, meta *transcript.Meta, seq int, id, name, input string) {
	t.Helper()
	items := feedTraexLine(t, meta, seq, "response_item", map[string]any{
		"type": "function_call", "call_id": id, "name": name, "arguments": input,
	})
	if len(items) != 0 {
		t.Fatalf("response_item resulted in duplicate items: %+v", items)
	}
}

func feedTraexCommand(t *testing.T, meta *transcript.Meta, seq int, id, source, output string) []proto.Item {
	t.Helper()
	return feedTraexLine(t, meta, seq, "event_msg", map[string]any{
		"type": "item_completed", "turn_id": "turn-1",
		"item": map[string]any{
			"type": "CommandExecution", "id": id, "source": source, "command": []string{"native command"},
			"status": "completed", "aggregated_output": output, "exit_code": 0,
			"duration": map[string]int{"secs": 0, "nanos": 125000000},
		},
	})
}

func feedTraexResult(t *testing.T, meta *transcript.Meta, seq int, id, output string) []proto.Item {
	t.Helper()
	return feedTraexLine(t, meta, seq, "response_item", map[string]any{
		"type": "function_call_output", "call_id": id, "output": output,
	})
}

func feedTraexLine(t *testing.T, meta *transcript.Meta, seq int, kind string, payload map[string]any) []proto.Item {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"timestamp": "2026-01-01T00:00:00Z", "type": kind, "payload": payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return transcript.ParseLine(seq, raw, meta)
}
