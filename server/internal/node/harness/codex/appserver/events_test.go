package appserver

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

func TestDecodeEventKeepsDirectAndWrappedThreadItems(t *testing.T) {
	t.Parallel()
	item := `{"type":"fileChange","id":"item-1","changes":[{"path":"example.txt"}]}`
	for _, payload := range []string{item, `{"payload":` + item + `}`} {
		ev, ok := DecodeEvent("item/started", []byte(`{"threadId":"thread-1","item":`+payload+`}`))
		if !ok || ev.Kind != ItemStarted || ev.ThreadID != "thread-1" || ev.Item.ID != "item-1" ||
			!slices.Equal(ev.Item.Paths(), []string{"example.txt"}) || string(ev.Item.Raw) != item {
			t.Fatalf("event = %+v, decoded = %t", ev, ok)
		}
	}
}

func TestThreadItemProjectionAgreesAcrossHistorySources(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	thread := []struct {
		name, app, rollout string
	}{
		{"prompt", `{"type":"userMessage","id":"user-1","content":[{"type":"text","text":"Check the release"}]}`,
			`{"type":"UserMessage","id":"user-1","content":[{"type":"text","text":"Check the release"}]}`},
		{"plan", `{"type":"plan","id":"plan-1","text":"Verify the build"}`,
			`{"type":"Plan","id":"plan-1","text":"Verify the build"}`},
		{"reasoning", `{"type":"reasoning","id":"thought-1","summary":["Check both sources"]}`,
			`{"type":"Reasoning","id":"thought-1","summary_text":["Check both sources"]}`},
		{"command", `{"type":"commandExecution","id":"call-1","command":"make check","status":"completed","aggregatedOutput":"ok","exitCode":0,"durationMs":125}`,
			`{"type":"CommandExecution","id":"call-1","command":["make check"],"status":"completed","aggregated_output":"ok","exit_code":0,"duration":{"secs":0,"nanos":125000000}}`},
		{"edit", `{"type":"fileChange","id":"edit-1","status":"completed","changes":[{"path":"example.txt","kind":{"type":"update"},"diff":"@@ -1 +1 @@\n-before\n+after"}]}`,
			`{"type":"FileChange","id":"edit-1","status":"completed","changes":{"example.txt":{"type":"update","unified_diff":"@@ -1 +1 @@\n-before\n+after"}}}`},
		{"subagent", `{"type":"subAgentActivity","id":"child-1","kind":"started","agentThreadId":"child-thread","agentPath":"/review"}`,
			`{"type":"SubAgentActivity","id":"child-1","kind":"started","agent_thread_id":"child-thread","agent_path":"/review"}`},
		{"terminal", `{"type":"terminalInteraction","id":"input-1","processId":"process-1","command":"\n"}`,
			`{"type":"TerminalInteraction","id":"input-1","process_id":"process-1","command":"\n"}`},
		{"reply", `{"type":"agentMessage","id":"reply-1","text":"The build passed.","phase":"final_answer"}`,
			`{"type":"AgentMessage","id":"reply-1","content":[{"type":"text","text":"The build passed."}],"phase":"final_answer"}`},
	}
	rolloutMeta, indexedMeta := &transcript.Meta{}, &transcript.Meta{}
	for i, tc := range thread {
		at := when.Add(time.Duration(i) * time.Second)
		seq := i + 2
		raw, err := json.Marshal(map[string]any{
			"timestamp": at, "type": "event_msg", "payload": map[string]any{
				"type": "item_completed", "turn_id": "turn-1", "item": json.RawMessage(tc.rollout),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := transcript.ParseLine(seq, raw, rolloutMeta)
		if len(want) == 0 {
			t.Errorf("%s: rollout item was lost", tc.name)
			continue
		}
		indexed, ok := codex.ThreadItemRecord(at, "turn-1", "", []byte(tc.app))
		if !ok {
			t.Fatalf("%s: SQLite item did not decode", tc.name)
		}
		ev, ok := DecodeEvent("item/completed", []byte(`{"threadId":"thread-1","turnId":"turn-1","item":`+tc.app+`}`))
		if !ok {
			t.Fatalf("%s: app-server item did not decode", tc.name)
		}
		if got := transcript.ProjectCodexRecord(seq, indexed, indexedMeta); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: indexed items = %+v, want %+v", tc.name, got, want)
		}
		if got := ev.CompletedItems(seq, at); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: app-server items = %+v, want %+v", tc.name, got, want)
		}
	}
}

func TestDecodeEventClassifiesTurnCompletion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		raw         string
		failureKind string
		hasFailure  bool
		modelRetry  bool
	}{
		{"success", `{"status":"completed"}`, "", false, false},
		{"interrupted", `{"status":"interrupted","error":{"message":"ignored"}}`, "", false, false},
		{"no detail", `{"status":"failed"}`, "", true, false},
		{"overloaded", `{"status":"failed","error":{"message":"Overloaded","codexErrorInfo":"serverOverloaded"}}`, proto.TurnErrorTransient, true, true},
		{"credentials", `{"status":"failed","error":{"message":"Invalid authorization","codex_error_info":"unauthorized"}}`, proto.TurnErrorUser, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev, ok := DecodeEvent("turn/completed", []byte(`{"threadId":"thread-1","turn":`+tc.raw+`}`))
			if !ok || ev.Kind != TurnCompleted || (ev.Failure != "") != tc.hasFailure ||
				ev.FailureKind != tc.failureKind || ev.ModelServiceFailure != tc.modelRetry {
				t.Fatalf("completion = %+v, decoded = %t", ev, ok)
			}
		})
	}
}

func TestInteractionIDsRemainStableAcrossDecoderChange(t *testing.T) {
	t.Parallel()
	if got := ApprovalID("session", "method", "item", "approval", "server"); got != "ix_3be2cf94dca0fbb9" {
		t.Fatalf("assistant approval ID = %s", got)
	}
	want := "codex-df3a94bbe66441fd6423aa14928d898a45e0c35ea592948452b399d5d5de7107"
	if got := ObservedRequestID("machine", "instance", "generation", "thread", []byte("42")); got != want {
		t.Fatalf("observed request ID = %s", got)
	}
}
