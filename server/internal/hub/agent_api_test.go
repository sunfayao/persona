package hub

import "testing"

// What a plugin's /agent asks of the hub: a new conversation under a
// purpose the hub knows on one of its machines (the assistant's route: anything else is refused before starting), and
// the hub's own lark_agent_reply out of the way while a plugin serves Feishu.
func TestAgentAPIForExtensions(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	mux := h.routes()
	tok := h.cfg.Token
	if code, out := call(t, mux, "POST", "/api/v1/assistant/conversations", tok, `{"prompt": "x", "purpose": "no.such"}`); code != 400 {
		t.Fatalf("unknown purpose %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/assistant/conversations", tok, `{"prompt": "x", "purpose": "lark.agent", "machine_id": "nowhere"}`); code != 400 {
		t.Fatalf("unknown machine %d %v", code, out)
	}

}
