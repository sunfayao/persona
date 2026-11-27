package extmgr

import (
	"testing"
)

// When Feishu refuses the paired card, its body still arrives as rich text.
func TestBotPairedCardFallbackKeepsBody(t *testing.T) {
	w := newBotWorld(t)
	w.h.opts.AssistantEvent = nil // no assistant to greet: the manager posts the paired card
	w.api.rejectInteractive = 230099
	_, out := w.do(t, "POST", "/api/v1/bots/feishu/pair", "")
	pairing := out["pairing"].(map[string]any)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_pair", "ou_me", "text", textContent(pairing["code"].(string)), nil))
	w.api.find(t, `"msg_type":"post"`, "以后在这里说的话会交给 Persona")
}
