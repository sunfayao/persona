package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/conversationlog"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

func activatePair(t *testing.T, d *db.DB) *db.LogicalConversation {
	t.Helper()
	ctx := t.Context()
	if err := d.ActivateConversationSegment(ctx, "server", "side-chat", "operation", 1); err != nil {
		t.Fatal(err)
	}
	c, err := d.LogicalConversation(ctx, "server", "side-chat")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLogicalSummaryWaitsOnPausedFakeModel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	_, err := waitLogicalSummary(ctx, func() (assistantwire.ModelCompleteResult, error) {
		calls++
		cancel()
		return assistantwire.ModelCompleteResult{}, &rpc.Error{Code: assistantwire.CodeModelPaused, Message: "fake quota pause", Data: json.RawMessage(`{"until":"2099-01-01T00:00:00Z"}`)}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("pause polled the fake model: %d %v", calls, err)
	}
	calls = 0
	out, err := waitLogicalSummary(t.Context(), func() (assistantwire.ModelCompleteResult, error) {
		calls++
		return assistantwire.ModelCompleteResult{Text: "fake summary"}, nil
	})
	if err != nil || calls != 1 || out.Text != "fake summary" {
		t.Fatalf("summary: %+v %v", out, err)
	}
}

func TestLogicalOperationReceiptIsScopedToConversation(t *testing.T) {
	first := &db.LogicalConversation{MachineID: "m", SessionID: "first"}
	second := &db.LogicalConversation{MachineID: "m", SessionID: "second"}
	if logicalNodeOperation(first, "same-id") == logicalNodeOperation(second, "same-id") {
		t.Fatal("two conversations shared a physical start receipt")
	}
	if logicalNodeOperation(first, "same-id") != logicalNodeOperation(first, "same-id") {
		t.Fatal("receipt changed after restart")
	}
}

func TestLogicalPhysicalDeliveryRequiresPositiveRefusalEvidence(t *testing.T) {
	for _, tc := range []struct {
		err     error
		refused bool
	}{
		{errors.New("connection lost after physical dispatch"), false},
		{context.DeadlineExceeded, false},
		{rpc.ErrClosed, false},
		{apierr.ErrOffline, true},
		{rpc.ErrNotSent, true},
		{&rpc.Error{Code: "assistant_supervision_runtime", Message: "policy refused", Data: json.RawMessage(`{"delivered":"none"}`)}, true},
	} {
		if got := logicalNotTaken(tc.err); got != tc.refused {
			t.Fatalf("%v: refused=%v want=%v", tc.err, got, tc.refused)
		}
	}
}

func TestLogicalInputIdentityDoesNotIncludeAutomaticRole(t *testing.T) {
	first, err := marshalLogicalInput(t.Context(), DeviceSessionSend, map[string]any{"text": "same content", "client_msg_id": "same-id", "supervision_instructions": "old role"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalLogicalInput(t.Context(), DeviceSessionSend, map[string]any{"text": "same content", "client_msg_id": "same-id", "supervision_instructions": "new role"})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || strings.Contains(string(first), "role") {
		t.Fatalf("automatic policy retained in input identity: %s / %s", first, second)
	}
}

func logicalPair(t *testing.T, engine string, source ...string) (*Service, *fakeHub, *db.DB, *db.LogicalConversation) {
	t.Helper()
	s, f, d, _ := historyFixture(t, func(string, ...any) {})
	src := "claude"
	if len(source) > 0 {
		src = source[0]
	}
	ctx := t.Context()
	for _, method := range []string{"assistant.segment.restore", "assistant.segment.retire"} {
		f.answerNode(method, func(string, json.RawMessage) (any, error) { return map[string]bool{"ok": true}, nil })
	}
	f.setSessions("server", proto.Session{SessionID: "side-chat", Assistant: true, Agent: src, Rev: 100}, proto.Session{SessionID: "target-chat", Assistant: true, Agent: engine, Rev: 100})
	if err := d.AdoptConversation(ctx, "server", "side-chat", "one", src, "h1"); err != nil {
		t.Fatal(err)
	}
	p, _ := json.Marshal(logicalSwitchRequest{OperationID: "operation", ExpectedEpoch: 1, MachineID: "server", Launcher: engine, Model: "target-model", Effort: "medium"})
	if err := d.BeginConversationSwitch(ctx, "server", "side-chat", db.ConversationSwitch{OperationID: "operation", ExpectedEpoch: 1, SourceSegment: "one", Request: p}); err != nil {
		t.Fatal(err)
	}
	if err := d.PutConversationHandoff(ctx, "server", "side-chat", "operation", json.RawMessage(`{"summary":"fake source summary","recent":[{"role":"user","text":"same conversation"}]}`)); err != nil {
		t.Fatal(err)
	}
	if err := d.PrepareConversationSegment(ctx, "server", "side-chat", "operation", conversationlog.Segment{ID: "two", MachineID: "server", SessionID: "target-chat", Engine: engine, SourceHistory: "h2", Launcher: engine, Model: "target-model", Effort: "medium"}, 6); err != nil {
		t.Fatal(err)
	}
	c, err := d.LogicalConversation(ctx, "server", "side-chat")
	if err != nil {
		t.Fatal(err)
	}
	return s, f, d, c
}
