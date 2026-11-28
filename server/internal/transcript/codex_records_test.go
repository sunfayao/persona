package transcript

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestCodexTaskCompletionKeepsFirstTokenTiming(t *testing.T) {
	t.Parallel()
	var m Meta
	input := `{"timestamp":"2026-01-01T00:01:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","duration_ms":480,"time_to_first_token_ms":95}}`
	items := ParseLine(1, []byte(input), &m)
	if len(items) != 1 || items[0].TurnDuration == nil || items[0].TurnDuration.DurationMS != 480 ||
		items[0].TTFTMS == nil || *items[0].TTFTMS != 95 {
		t.Fatalf("completion timing = %+v", items)
	}
}

func TestCodexPlanItemKeepsNativeText(t *testing.T) {
	t.Parallel()
	var m Meta
	input := `{"timestamp":"2026-01-01T00:01:00Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"Plan","id":"item-1","text":"1. Check the inputs\n2. Run the tests"}}}`
	items := ParseLine(1, []byte(input), &m)
	if len(items) != 1 || items[0].Kind != proto.KindText || !items[0].Narration || items[0].Text != "1. Check the inputs\n2. Run the tests" {
		t.Fatalf("plan item = %+v", items)
	}
}

func TestCodexTaskNotificationUsesNativeFields(t *testing.T) {
	t.Parallel()
	var m Meta
	input := `{"timestamp":"2026-01-01T00:01:00Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"TaskNotification","id":"item-1","task_notification":{"type":"shell","taskId":"task-11","status":"failed","summary":"Build exited with an error","exitCode":11,"outputFile":"task-11.log"}}}}`
	items := ParseLine(1, []byte(input), &m)
	if len(items) != 1 || items[0].Kind != proto.KindNotice || !items[0].Wake {
		t.Fatalf("task notification = %+v", items)
	}
	note := items[0].Task
	if note == nil || note.Kind != "shell" || note.TaskID != "task-11" || note.Status != "failed" ||
		note.ExitCode == nil || *note.ExitCode != 11 || note.OutputFile != "task-11.log" {
		t.Fatalf("task fields = %+v", note)
	}
}

func TestCodexUsageRecordAndTokenCountRepresentOneRequest(t *testing.T) {
	t.Parallel()
	var m Meta
	for seq, input := range []string{
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"token_usage_record","payload":{"usage":{"input_tokens":90,"cached_input_tokens":40,"output_tokens":10},"thread_token_usage":{"total_tokens":100,"output_tokens":10}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":100,"output_tokens":10},"last_token_usage":{"input_tokens":90,"cached_input_tokens":40,"output_tokens":10},"model_context_window":300}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":50,"output_tokens":8},"last_token_usage":{"input_tokens":42,"output_tokens":8}}}}`,
	} {
		ParseLine(seq+1, []byte(input), &m)
	}
	if m.ContextTokens != 90 || m.OutputTokens != 10 || m.ContextWindow != 300 {
		t.Fatalf("usage after compatibility record: context=%d, output=%d, window=%d", m.ContextTokens, m.OutputTokens, m.ContextWindow)
	}
	input := `{"timestamp":"2026-01-01T00:00:05Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","duration_ms":480}}`
	items := ParseLine(5, []byte(input), &m)
	if len(items) != 1 || len(items[0].Usage) != 1 {
		t.Fatalf("completion usage = %+v", items)
	}
	u := items[0].Usage[0]
	if u.Input != 50 || u.Output != 10 || u.CacheRead != 40 {
		t.Fatalf("request usage = %+v", u)
	}
}
