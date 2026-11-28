package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func subagentJSON(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		panic(err)
	}
	return strings.TrimSpace(b.String())
}

func subagentLaunch() []string {
	return []string{
		`{"type":"user","uuid":"prompt","timestamp":"2026-01-02T10:00:00Z","message":{"role":"user","content":"Run a synthetic review."}}`,
		`{"type":"assistant","uuid":"launch","timestamp":"2026-01-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_review","name":"Agent","input":{"description":"Synthetic review","prompt":"Review the synthetic fixture."}}]}}`,
		`{"type":"user","uuid":"receipt","timestamp":"2026-01-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_review","content":"Async agent launched successfully. See the agentId below.\nagentId: agent-review"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-review"}}`,
	}
}

func subagentPeerLine(id, sender, body string, queued, handback bool) string {
	origin := map[string]any{"kind": "peer", "from": sender, "senderTaskId": sender, "name": "general-purpose", "body": body}
	if handback {
		origin["handback"], origin["handbackReportText"] = true, body
	}
	line := map[string]any{"uuid": id, "timestamp": "2026-01-02T10:00:03Z"}
	if queued {
		line["type"] = "attachment"
		line["attachment"] = map[string]any{"type": "queued_command", "commandMode": "prompt", "origin": origin, "prompt": body}
	} else {
		line["type"], line["isMeta"], line["origin"] = "user", true, origin
		line["message"] = map[string]any{"role": "user", "content": body}
	}
	return subagentJSON(line)
}

func subagentNoteLine(id, status, result string, queued bool) string {
	text := `<task-notification><task-id>agent-review</task-id><status>` + status + `</status><summary>Agent "Synthetic review" finished</summary><result>` + result + `</result></task-notification>`
	line := map[string]any{"uuid": id, "timestamp": "2026-01-02T10:00:04Z"}
	if queued {
		line["type"] = "attachment"
		line["attachment"] = map[string]any{"type": "queued_command", "commandMode": "task-notification", "prompt": text}
	} else {
		line["type"] = "user"
		line["message"] = map[string]any{"role": "user", "content": text}
	}
	return subagentJSON(line)
}

func TestSubagentMessagesAreAssociatedToolResults(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued=%t", queued), func(t *testing.T) {
			var meta Meta
			for i, line := range subagentLaunch() {
				got := ParseLine(i+1, []byte(line), &meta)
				if i == 2 && (len(got) != 1 || got[0].Task == nil || got[0].Task.Status != "running") {
					t.Fatalf("launch receipt: %+v", got)
				}
			}
			got := ParseLine(4, []byte(subagentPeerLine("interim", "agent-review", "Interim analysis; continue working.", queued, false)), &meta)
			if len(got) != 1 || got[0].Kind != proto.KindToolResult || got[0].From != "" || got[0].ToolUseID != "toolu_review" || got[0].Task.Status != "running" || got[0].Wake == queued || got[0].Queued {
				t.Fatalf("interim report: %+v", got)
			}
			if len(meta.Background) != 1 || meta.Turns != 1 || meta.FirstUser != "Run a synthetic review." {
				t.Fatalf("interim report changed running state or user turns: %+v", meta)
			}
			waiting := ParseLine(5, []byte(subagentNoteLine("waiting", "waiting", "", queued)), &meta)
			if len(waiting) != 1 || waiting[0].Kind != proto.KindToolResult || len(meta.Background) != 1 {
				t.Fatalf("waiting report ended background work: %+v %+v", waiting, meta.Background)
			}
			done := ParseLine(6, []byte(subagentPeerLine("handback", "agent-review", "Finished the synthetic review.", queued, true)), &meta)
			if len(done) != 1 || done[0].Kind != proto.KindToolResult || done[0].Task.Status != "completed" || done[0].Text != "Finished the synthetic review." || len(meta.Background) != 0 {
				t.Fatalf("completed handback: %+v", done)
			}
			pointer := "This agent's report was delivered to you (its SubagentHandback call)."
			note := ParseLine(7, []byte(subagentNoteLine("notification", "completed", pointer, queued)), &meta)
			if len(note) != 1 || note[0].Kind != proto.KindToolResult || note[0].ToolUseID != "toolu_review" || note[0].Text != "" || note[0].Task.Detail != "" || note[0].Wake == queued {
				t.Fatalf("completion notification: %+v", note)
			}
			if duplicates := ParseLine(8, []byte(subagentNoteLine("duplicate", "completed", pointer, queued)), &meta); len(duplicates) != 0 {
				t.Fatalf("duplicate completion: %+v", duplicates)
			}
		})
	}
}

func TestSubagentAssociationDoesNotUsePeerDisplayName(t *testing.T) {
	var meta Meta
	for i, line := range subagentLaunch() {
		ParseLine(i+1, []byte(line), &meta)
	}
	for _, queued := range []bool{false, true} {
		got := ParseLine(4, []byte(subagentPeerLine("unrelated", "unrelated-session", "A genuine message from another session.", queued, false)), &meta)
		if len(got) != 1 || got[0].Kind != proto.KindText || got[0].From != "general-purpose" || got[0].Text != "A genuine message from another session." {
			t.Fatalf("unrelated peer changed: %+v", got)
		}
	}
}

func TestIdleSubagentReportRetainsWakeBoundary(t *testing.T) {
	var meta Meta
	for i, line := range subagentLaunch() {
		ParseLine(i+1, []byte(line), &meta)
	}
	ParseLine(4, []byte(`{"type":"assistant","uuid":"answer","message":{"role":"assistant","content":[{"type":"text","text":"The preliminary response is complete."}]}}`), &meta)
	ParseLine(5, []byte(`{"type":"system","subtype":"turn_duration","uuid":"end","parentUuid":"answer","durationMs":1000}`), &meta)
	if !meta.TurnClosed {
		t.Fatal("the parent turn did not end")
	}
	promptAt := meta.LastPromptAt
	got := ParseLine(6, []byte(subagentPeerLine("idle-report", "agent-review", "The subagent is still checking.", false, false)), &meta)
	if len(got) != 1 || !got[0].Wake || got[0].Kind != proto.KindToolResult || !meta.TurnOpen || meta.TurnClosed || meta.Turns != 1 || meta.LastPromptAt != promptAt {
		t.Fatalf("idle peer delivery lost its wake or became user input: %+v", got)
	}
}

func TestSubagentNotificationLifecycle(t *testing.T) {
	for _, status := range []string{"running", "waiting", "future-status", "completed", "failed", "killed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			var meta Meta
			for i, line := range subagentLaunch() {
				ParseLine(i+1, []byte(line), &meta)
			}
			got := ParseLine(4, []byte(subagentNoteLine("outcome", status, "Synthetic status detail.", true)), &meta)
			if len(got) != 1 || got[0].Kind != proto.KindToolResult || got[0].Task.Status != status || got[0].IsError != (status == "failed") {
				t.Fatalf("notification: %+v", got)
			}
			if (len(meta.Background) == 0) != agentTerminal(status) {
				t.Fatalf("status %s incorrectly changed background lifecycle: %+v", status, meta.Background)
			}
			late := ParseLine(5, []byte(subagentPeerLine("late-status", "agent-review", "More synthetic detail.", true, false)), &meta)
			if len(late) != 1 || late[0].Task.Status != status || late[0].IsError != (status == "failed") {
				t.Fatalf("late report lost lifecycle: %+v", late)
			}
		})
	}
}

func TestSubagentSeedsSkipLargeRequestBlobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.jsonl")
	data := `{"type":"api-request-blob","tools":[{"name":"Agent"}],"padding":"` + strings.Repeat("x", 100<<10) + "\"}\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, seeds, _, _, _, _, _, err := scanLines(f, int64(len(data)))
	if err != nil || len(seeds) != 0 {
		t.Fatalf("request blob should not become expensive agent state seed: %+v %v", seeds, err)
	}
}

func TestSubagentForegroundAndFailedLaunch(t *testing.T) {
	for _, failed := range []bool{false, true} {
		var meta Meta
		ParseLine(1, []byte(subagentLaunch()[1]), &meta)
		line := subagentJSON(map[string]any{"type": "user", "uuid": "foreground", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_review", "content": "Synthetic foreground result.", "is_error": failed}}}})
		got := ParseLine(2, []byte(line), &meta)
		want := "completed"
		if failed {
			want = "failed"
		}
		if len(got) != 1 || got[0].Task == nil || got[0].Task.Status != want || len(meta.Background) != 0 {
			t.Fatalf("foreground result: %+v", got)
		}
	}
}

func TestSubagentResultsWindowLiveAndFullText(t *testing.T) {
	old := windowBytes
	defer func() { windowBytes = old }()
	windowBytes = 4 << 10
	lines := subagentLaunch()
	long := strings.Repeat("Synthetic report detail.\n", 600)
	lines = append(lines, subagentPeerLine("large-report", "agent-review", long, true, false))
	for i := range 10 {
		lines = append(lines, subagentJSON(map[string]any{"type": "file-history-snapshot", "uuid": fmt.Sprintf("padding-%d", i), "padding": strings.Repeat("x", 3000)}))
	}
	lines = append(lines, subagentNoteLine("finished", "completed", "All synthetic checks pass.", true))
	path := filepath.Join(t.TempDir(), "synthetic.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	windowed := NewFile(path)
	if _, _, err := windowed.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	if windowed.base == 0 {
		t.Fatal("expected a cold tail window")
	}
	appendLine := subagentPeerLine("late-message", "agent-review", "Additional completed result detail.", true, false) + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(appendLine)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	live, _, err := windowed.RefreshSpan()
	if err != nil || len(live) != 1 || live[0].Kind != proto.KindToolResult || live[0].Task.Status != "completed" {
		t.Fatalf("late incremental message: %+v %v", live, err)
	}
	windowBytes = 1 << 40
	whole := NewFile(path)
	if _, _, err := whole.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	got, want := allItems(windowed), allItems(whole)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("window/live and whole read differ:\nwindow %+v\nwhole %+v", got, want)
	}
	for _, it := range got {
		if it.ID != "large-report#0" {
			continue
		}
		if it.FullSize <= pageOutputLen || len(it.Task.Detail) > pageOutputLen+100 {
			t.Fatalf("unbounded report preview or no full-text marker: %+v", it)
		}
		full, err := windowed.Full(it.ID)
		if err != nil || full.Text != strings.TrimSpace(long) || full.Kind != proto.KindToolResult || full.ToolUseID != "toolu_review" {
			t.Fatalf("full report lost: kind=%s text=%d err=%v", full.Kind, len(full.Text), err)
		}
		return
	}
	t.Fatal("large report not retained")
}

// A window that names a subagent only by its messages (senderTaskId), with the agent's failure notification before
// it, keeps the failure when a later message of the agent arrives: the later lines used to read the launch again
// (back to running) while the failure was dropped as a notification already shown.
func TestSubagentStatusFromMessagesBeforeWindowSurvivesLiveMessage(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued=%t", queued), func(t *testing.T) {
			old := windowBytes
			defer func() { windowBytes = old }()
			windowBytes = 4 << 10
			lines := append(subagentLaunch(), subagentNoteLine("failed", "failed", "Synthetic failure.", queued))
			for i := range 10 {
				lines = append(lines, subagentJSON(map[string]any{"type": "file-history-snapshot", "uuid": fmt.Sprintf("padding-%d", i), "padding": strings.Repeat("x", 3000)}))
			}
			lines = append(lines, subagentPeerLine("resumed", "agent-review", "Resumed and still working.", queued, false))
			path := filepath.Join(t.TempDir(), "synthetic.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			windowed := NewFile(path)
			fresh, _, err := windowed.RefreshSpan()
			if err != nil {
				t.Fatal(err)
			}
			if windowed.base == 0 || len(fresh) != 1 || fresh[0].Task == nil || fresh[0].Task.Status != "failed" {
				t.Fatalf("the window's message (base %d): %+v", windowed.base, fresh)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString(subagentPeerLine("later", "agent-review", "A later message.", queued, false) + "\n")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			live, _, err := windowed.RefreshSpan()
			if err != nil || len(live) != 1 || live[0].Task == nil || live[0].Task.Status != "failed" || !live[0].IsError {
				t.Fatalf("the later message: %+v %v", live, err)
			}
			windowBytes = 1 << 40
			whole := NewFile(path)
			if _, _, err := whole.RefreshSpan(); err != nil {
				t.Fatal(err)
			}
			if got, want := allItems(windowed), allItems(whole); !reflect.DeepEqual(got, want) {
				t.Fatalf("window/live and whole read differ:\nwindow %+v\nwhole %+v", got, want)
			}
		})
	}
}

type syntheticClaudeReader struct{}

func (syntheticClaudeReader) DecodeLine(line []byte, dst *ClaudeLine) error {
	if err := unmarshalLine(line, dst); err != nil {
		return err
	}
	var raw struct {
		ToolUseResult struct {
			IsAsync        bool   `json:"isAsync"`
			Status         string `json:"status"`
			AgentID        string `json:"agentId"`
			ResumedAgentID string `json:"resumedAgentId"`
		} `json:"toolUseResult"`
	}
	if json.Unmarshal(line, &raw) == nil {
		dst.TaskEvidence = ToolTaskEvidence{
			AgentID:      raw.ToolUseResult.AgentID,
			AgentAsync:   raw.ToolUseResult.IsAsync || raw.ToolUseResult.Status == "async_launched",
			ResumedAgent: raw.ToolUseResult.ResumedAgentID,
		}
	}
	return nil
}

func TestBackgroundAgentDidntFinishClosesSubagentAfterResume(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00Z","message":{"role":"user","content":"Check service roles."}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-01-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_roles","name":"Agent","input":{"description":"Synthetic role audit","prompt":"Inspect roles."}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-01-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_roles","content":"Async agent launched.\nagentId: agent-roles"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-roles"}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-01-02T10:05:00Z","message":{"role":"user","content":"<task-notification><task-id>agent-roles</task-id><status>completed</status><summary>Agent \"Synthetic role audit\" finished</summary><result>Done.</result></task-notification>"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-01-02T10:10:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_msg","name":"SendMessage","input":{"to":"agent-roles","message":"Double check"}}]}}`,
		`{"type":"user","uuid":"u4","timestamp":"2026-01-02T10:10:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_msg","content":"Message sent"}]},"toolUseResult":{"success":true,"resumedAgentId":"agent-roles"}}`,
		`{"type":"user","uuid":"u5","timestamp":"2026-01-02T10:30:00Z","message":{"role":"user","content":"<task-notification><task-id>agent-roles</task-id><status>stopped</status><summary>Background agent \"Synthetic role audit\" didn't finish before the previous session ended</summary><note>No completion record was found after re-dispatch.</note></task-notification>"}}`,
	}
	path := filepath.Join(t.TempDir(), "resumed.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewParsedFile(path, Sources{Claude: syntheticClaudeReader{}, Agent: "claude"})
	if _, _, err := f.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	snap := f.TurnSnapshot()
	if len(snap.Subagents) != 1 {
		t.Fatalf("subagents = %+v, want 1", snap.Subagents)
	}
	got := snap.Subagents[0]
	if got.ID != "agent-roles" || got.Running || got.Status != proto.SubagentStatusCancelled {
		t.Fatalf("resumed interrupted subagent = %+v, want Running=false Status=cancelled", got)
	}
	if len(f.Meta().Background) != 0 {
		t.Fatalf("meta.Background = %+v, want empty", f.Meta().Background)
	}
}

func TestBatchedBackgroundAgentsDidntFinishClosesAll(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00Z","message":{"role":"user","content":"Run three audits."}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-01-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"First audit","prompt":"a"}},{"type":"tool_use","id":"toolu_2","name":"Agent","input":{"description":"Second audit","prompt":"b"}},{"type":"tool_use","id":"toolu_3","name":"Agent","input":{"description":"Third audit","prompt":"c"}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-01-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Async agent launched.\nagentId: agent-one"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-one"}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-01-02T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"Async agent launched.\nagentId: agent-two"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-two"}}`,
		`{"type":"user","uuid":"u4","timestamp":"2026-01-02T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"Async agent launched.\nagentId: agent-three"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-three"}}`,
		`{"type":"user","uuid":"u5","timestamp":"2026-01-02T10:20:00Z","message":{"role":"user","content":"<task-notification>\n<task-id>agent-one</task-id>\n<task-id>agent-two</task-id>\n<task-id>agent-three</task-id>\n<status>stopped</status>\n<summary>3 background agents didn't finish before the previous session ended: \"First audit\" (agent-one), \"Second audit\" (agent-two), \"Third audit\" (agent-three)</summary>\n</task-notification>"}}`,
	}
	path := filepath.Join(t.TempDir(), "batched.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewParsedFile(path, Sources{Claude: syntheticClaudeReader{}, Agent: "claude"})
	if _, _, err := f.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	snap := f.TurnSnapshot()
	if len(snap.Subagents) != 3 {
		t.Fatalf("subagents = %+v, want 3", snap.Subagents)
	}
	for _, agent := range snap.Subagents {
		if agent.Running || agent.Status != proto.SubagentStatusCancelled {
			t.Fatalf("batched agent %s still active: %+v", agent.ID, agent)
		}
	}
	if len(f.Meta().Background) != 0 {
		t.Fatalf("meta.Background = %+v, want empty", f.Meta().Background)
	}
}

func TestUnknownNotificationSummaryPromotesKnownAgentAndTaskStopCancels(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00Z","message":{"role":"user","content":"Start two agents."}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-01-02T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_a","name":"Agent","input":{"description":"First helper","prompt":"a"}},{"type":"tool_use","id":"toolu_b","name":"Agent","input":{"description":"Second helper","prompt":"b"}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-01-02T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"Async agent launched.\nagentId: agent-alpha"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-alpha"}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-01-02T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_b","content":"Async agent launched.\nagentId: agent-beta"}]},"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"agent-beta"}}`,
		`{"type":"user","uuid":"u4","timestamp":"2026-01-02T10:05:00Z","message":{"role":"user","content":"<task-notification><task-id>agent-alpha</task-id><status>stopped</status><summary>Unrecognized future notice text</summary></task-notification>"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-01-02T10:06:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_stop","name":"TaskStop","input":{"task_id":"agent-beta"}}]}}`,
		`{"type":"user","uuid":"u5","timestamp":"2026-01-02T10:06:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_stop","content":"Stopped task agent-beta"}]}}`,
	}
	path := filepath.Join(t.TempDir(), "fallback.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewParsedFile(path, Sources{Claude: syntheticClaudeReader{}, Agent: "claude"})
	if _, _, err := f.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	snap := f.TurnSnapshot()
	if len(snap.Subagents) != 2 {
		t.Fatalf("subagents = %+v, want 2", snap.Subagents)
	}
	for _, agent := range snap.Subagents {
		if agent.Running || agent.Status != proto.SubagentStatusCancelled {
			t.Fatalf("agent %s not converged: %+v", agent.ID, agent)
		}
	}
}

func TestCodexSubagentActivityTerminalStatuses(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-02T10:00:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-01-02T10:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"SubAgentActivity","id":"sa-1","kind":"started","agent_thread_id":"thread-fail","agent_path":"/root/worker_a"}}}`,
		`{"timestamp":"2026-01-02T10:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"SubAgentActivity","id":"sa-2","kind":"failed","agent_thread_id":"thread-fail","agent_path":"/root/worker_a"}}}`,
		`{"timestamp":"2026-01-02T10:00:03Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"SubAgentActivity","id":"sa-3","kind":"started","agent_thread_id":"thread-stop","agent_path":"/root/worker_b"}}}`,
		`{"timestamp":"2026-01-02T10:00:04Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"SubAgentActivity","id":"sa-4","kind":"interrupted","agent_thread_id":"thread-stop","agent_path":"/root/worker_b"}}}`,
	}
	path := filepath.Join(t.TempDir(), "codex-subagents.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewParsedFile(path, Sources{Agent: "codex"})
	if _, _, err := f.RefreshSpan(); err != nil {
		t.Fatal(err)
	}
	snap := f.TurnSnapshot()
	if len(snap.Subagents) != 2 {
		t.Fatalf("codex subagents = %+v, want 2", snap.Subagents)
	}
	if snap.Subagents[0].Running || snap.Subagents[0].Status != proto.SubagentStatusFailed {
		t.Fatalf("failed codex subagent = %+v", snap.Subagents[0])
	}
	if snap.Subagents[1].Running || snap.Subagents[1].Status != proto.SubagentStatusCancelled {
		t.Fatalf("interrupted codex subagent = %+v", snap.Subagents[1])
	}
}
