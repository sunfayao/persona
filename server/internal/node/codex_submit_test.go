package node

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/screen"
)

func TestCodexSendPreservesMatchingDraft(t *testing.T) {
	const text = "An earlier synthetic assistant message."
	n, f, _ := codexNode(t, proto.StatusIdle)
	f.setScreen(cxPane, cxBox(text))
	n.typing.typed(cxPane, text)
	_, err := request(t, n, "session.send", map[string]any{"session_id": cxID, "text": text})
	if refusalCode(err) != screen.InputDraft {
		t.Fatalf("matching prior text is not permission to submit a draft: %v", err)
	}
	if calls := f.trace("agent.prompt", "pane.send_text", "pane.send_keys"); len(calls) != 0 {
		t.Fatalf("pre-existing draft was touched: %v", calls)
	}
}

func TestCodexNativeCommandOpensATerminalPanel(t *testing.T) {
	n, f, _ := codexNode(t, proto.StatusIdle)
	f.setScreen(cxPane, cxEmpty)
	f.after(func(c herdrCall) {
		if c.Method == "agent.prompt" {
			f.setScreen(cxPane, "Native review panel")
		}
	})
	params := map[string]any{"session_id": cxID, "text": "/review 123", "client_msg_id": "native-review"}
	for range 2 {
		if _, err := request(t, n, "session.send", params); err != nil {
			t.Fatalf("native command failed: %v", err)
		}
	}
	assertTrace(t, f.trace("agent.prompt", "pane.send_keys"), []string{
		call("agent.prompt", map[string]any{"target": cxPane, "text": "/review 123"}),
	})
}

func TestCodexNativeCommandLeavesARealDraftAlone(t *testing.T) {
	n, f, _ := codexNode(t, proto.StatusIdle)
	f.setScreen(cxPane, cxBox("Keep my draft."))
	_, err := request(t, n, "session.send", map[string]any{"session_id": cxID, "text": "/status"})
	if refusalCode(err) != screen.InputDraft || len(f.trace("agent.prompt", "pane.send_keys")) != 0 {
		t.Fatalf("native command must leave the draft alone, got %v", err)
	}
}
