package claude

import (
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

func TestClaudeTracksStructuredBackgroundResultsWithoutReceiptText(t *testing.T) {
	t.Parallel()
	meta := transcript.Meta{ClaudeReader: Reader{}}
	for _, tc := range []struct {
		call, tool, input, result, taskID, kind string
	}{
		{"shell-fg", "Bash", `{"command":"make check"}`, `{"backgroundTaskId":"shell-1"}`, "shell-1", "shell"},
		{"shell-bg", "Bash", `{"run_in_background":true,"command":"make check"}`, `{"backgroundTaskId":"shell-2"}`, "shell-2", "shell"},
		{"watch", "Monitor", `{"command":"tail example.log"}`, `{"taskId":"monitor-1","persistent":true}`, "monitor-1", "monitor"},
		{"launch", "Agent", `{"description":"Review"}`, `{"agentId":"agent-1","status":"async_launched"}`, "agent-1", "agent"},
	} {
		feedClaudeCall(t, &meta, tc.call, tc.tool, tc.input)
		feedClaudeResult(t, &meta, tc.call, tc.result)
		task := meta.Background[len(meta.Background)-1]
		if task.ID != tc.taskID || task.Kind != tc.kind {
			t.Fatalf("%s: task = %+v", tc.tool, task)
		}
	}
	if len(meta.Background) != 4 {
		t.Fatalf("background tasks = %+v", meta.Background)
	}
}

func TestClaudeResumedAgentKeepsOriginalLaunchAssociation(t *testing.T) {
	t.Parallel()
	meta := transcript.Meta{ClaudeReader: Reader{}}
	feedClaudeCall(t, &meta, "launch", "Agent", `{"description":"Review"}`)
	feedClaudeResult(t, &meta, "launch", `{"agentId":"agent-1","isAsync":true}`)
	feedClaudeCall(t, &meta, "resume", "SendMessage", `{"to":"agent-1","message":"Continue the check"}`)
	items := feedClaudeResult(t, &meta, "resume", `{"resumedAgentId":"agent-1"}`)
	if len(items) != 1 || items[0].Task == nil || items[0].Task.TaskID != "agent-1" ||
		items[0].Task.Status != "running" || items[0].Task.Title != "Review" {
		t.Fatalf("resume items = %+v", items)
	}
	const notice = `<task-notification><task-id>agent-1</task-id><status>completed</status><summary>Agent "Review" finished</summary><result>Review complete.</result></task-notification>`
	items = feedClaudeLine(t, &meta, "user", map[string]any{"content": notice}, nil)
	if len(items) != 1 || items[0].ToolUseID != "launch" {
		t.Fatalf("completion left its launch: %+v", items)
	}
}

func TestClaudeOmitsNativeArtifactAndForkContext(t *testing.T) {
	t.Parallel()
	const message = `<artifact-view-context id="demo">Artifact metadata</artifact-view-context><fork-boilerplate>Fork metadata</fork-boilerplate>Review these changes.`
	meta := transcript.Meta{ClaudeReader: Reader{}}
	items := feedClaudeLine(t, &meta, "user", map[string]any{"content": message}, nil)
	if len(items) != 1 || items[0].Text != "Review these changes." || transcript.Title(message) != items[0].Text {
		t.Fatalf("items = %+v, title = %q", items, transcript.Title(message))
	}
}

func feedClaudeCall(t *testing.T, meta *transcript.Meta, call, tool, input string) {
	t.Helper()
	feedClaudeLine(t, meta, "assistant", map[string]any{
		"content": []map[string]any{{"type": "tool_use", "id": call, "name": tool, "input": json.RawMessage(input)}},
	}, nil)
}

func feedClaudeResult(t *testing.T, meta *transcript.Meta, call, outcome string) []proto.Item {
	t.Helper()
	return feedClaudeLine(t, meta, "user", map[string]any{
		"content": []map[string]any{{"type": "tool_result", "tool_use_id": call, "content": "Accepted."}},
	}, json.RawMessage(outcome))
}

func feedClaudeLine(t *testing.T, meta *transcript.Meta, role string, message map[string]any, outcome json.RawMessage) []proto.Item {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": role, "timestamp": "2026-01-01T00:00:00Z", "uuid": role + "-line",
		"message": message, "toolUseResult": outcome,
	})
	if err != nil {
		t.Fatal(err)
	}
	return transcript.ParseLine(1, raw, meta)
}
