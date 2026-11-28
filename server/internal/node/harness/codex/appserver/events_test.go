package appserver

import (
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
	const atMS = 1767225600000
	const v2 = `{"type":"commandExecution","id":"call-1","command":"make check","status":"completed","aggregatedOutput":"ok","exitCode":0,"durationMs":125,"completedAtMs":1767225600000}`
	ev, ok := DecodeEvent("item/completed", []byte(`{"threadId":"thread-1","turnId":"turn-1","item":`+v2+`}`))
	if !ok {
		t.Fatal("app-server item did not decode")
	}
	at := time.UnixMilli(atMS).UTC()
	indexed, ok := codex.ThreadItemRecord(at, "turn-1", "commandExecution", []byte(v2))
	if !ok {
		t.Fatal("SQLite item did not decode")
	}
	const rollout = `{"timestamp":"2026-01-01T00:00:00Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"CommandExecution","id":"call-1","command":["make check"],"status":"completed","aggregated_output":"ok","exit_code":0,"duration":{"secs":0,"nanos":125000000}}}}`
	want := transcript.ParseLine(2, []byte(rollout), &transcript.Meta{})
	if len(want) != 2 {
		t.Fatalf("rollout items = %+v", want)
	}
	if got := transcript.ProjectCodexRecord(2, indexed, &transcript.Meta{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed items = %+v, want %+v", got, want)
	}
	if got := ev.CompletedItems(2, time.Time{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("app-server items = %+v, want %+v", got, want)
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
