package extmgr

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// The event content follows the documented received post shape, not the language-keyed send shape.
func TestPostAdjacentMentionCommandHandoff(t *testing.T) {
	for _, command := range []string{"/read", "/ai"} {
		t.Run(command, func(t *testing.T) {
			w := newBotWorld(t)
			w.pair(t)
			w.topicsOn(t, topicChat)
			w.readOn(t)
			content := `{"title":"","content":[[{"tag":"at","user_id":"@_user_1","user_name":""},{"tag":"text","text":"` + command + ` summarize"}]]}`
			w.send(t, botMessageEvent("om_adjacent", "ou_me", "post", content, map[string]any{"chat_type": "group", "chat_id": askChat, "mentions": atBot}))
			w.send(t, groupEvent("om_barrier", "ou_me", "marker", topicChat, "omt_marker"))
			eventually(t, 5*time.Second, "barrier handed", func() bool { got := w.handed(); return len(got) > 0 && got[len(got)-1] == "om_barrier" })
			got := w.handed()
			if len(got) != 2 || got[0] != "om_adjacent" {
				t.Fatalf("handoffs %v; want [om_adjacent om_barrier]", got)
			}
			expected := assistantwire.FeishuFromElsewhere
			if command == "/read" {
				expected = assistantwire.FeishuFromRead
			}
			messages := w.a.got(assistantwire.MethodFeishuMessage)
			if !containsFrom(messages[0], expected) {
				t.Fatalf("handoff %s; want from %q", messages[0], expected)
			}
		})
	}
}

func containsFrom(raw json.RawMessage, want string) bool {
	var message assistantwire.FeishuMessage
	_ = json.Unmarshal(raw, &message)
	return message.From == want
}

func TestPostCommandUnknownBotIdentityIsDenied(t *testing.T) {
	for _, command := range []string{"/read", "/ai"} {
		t.Run(command, func(t *testing.T) {
			w := newBotWorld(t)
			w.pair(t)
			w.topicsOn(t, topicChat)
			w.readOn(t)
			w.api.mu.Lock()
			w.api.botInfoFail = true
			w.api.mu.Unlock()
			w.h.bots.mu.Lock()
			w.h.bots.info.OpenID = ""
			w.h.bots.mu.Unlock()
			before := w.api.total()
			content := `{"content":[[{"tag":"at","user_id":"@_user_1"},{"tag":"text","text":"` + command + ` summarize"}]]}`
			w.send(t, botMessageEvent("om_unknown_post", "ou_me", "post", content, map[string]any{"chat_type": "group", "chat_id": askChat, "mentions": atBot}))
			w.send(t, groupEvent("om_unknown_barrier", "ou_me", "marker", topicChat, "omt_marker"))
			eventually(t, 5*time.Second, "barrier handed", func() bool { got := w.handed(); return len(got) > 0 && got[len(got)-1] == "om_unknown_barrier" })
			if got := w.handed(); len(got) != 1 || len(w.settings().AISources) != 0 {
				t.Fatalf("unknown identity acted: %v", got)
			}
			calls := w.api.since(before)
			if !strings.Contains(calls, "GET /open-apis/bot/v3/info") || strings.Contains(calls, "/open-apis/im/") {
				t.Fatalf("unexpected calls: %s", calls)
			}
		})
	}
}
