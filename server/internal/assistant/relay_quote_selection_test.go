package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func quoteSend(t *testing.T, d *dispatchPolicyHarness, from, to, selector string) proto.ToolResult {
	t.Helper()
	return d.tool(t, from, "conversation_send", `{"id":`+jsonString(to)+`,"text":"prepare the trail map","quote":`+jsonString(selector)+`}`)
}

// Each refusal leaves the node, the durable quote book and the relay rate alone.
func checkQuoteRefusal(t *testing.T, d *dispatchPolicyHarness, selector string, hints ...string) string {
	t.Helper()
	initial := d.f.sent()
	res := quoteSend(t, d, d.mainID, d.leaderID, selector)
	msg := res.Content[0].Text
	if !res.IsError {
		t.Fatalf("quote was accepted: %s", msg)
	}
	for _, hint := range hints {
		if !strings.Contains(msg, hint) {
			t.Errorf("refusal %q does not mention %q", msg, hint)
		}
	}
	var book json.RawMessage
	found, err := d.s.store().Setting(context.Background(), relayQuotesKey, &book)
	if err != nil || found || d.f.sent() != initial {
		t.Errorf("refusal sent %d messages, saved quote book = %t, read error = %v", d.f.sent()-initial, found, err)
	}
	d.s.relays.mu.Lock()
	defer d.s.relays.mu.Unlock()
	if len(d.s.relays.sent) != 0 || len(d.s.relays.quoted) != 0 {
		t.Error("refusal consumed a relay allowance")
	}
	return msg
}
