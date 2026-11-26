package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A track conversation's context (#13499): the time, the track and the owner's language, in each language.
func TestGoldenSpaceContext(t *testing.T) {
	now := time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC)
	for _, tc := range []struct{ name, language string }{{"zh-Hans", ""}, {"en", "English"}, {"ja", "日本語"}} {
		got := prompts.RenderSpaceContext(prompts.SpaceContext{Now: now, Zone: "UTC", Owner: "Example user", Title: "qa-plain", Language: tc.language})
		golden(t, "space_context."+tc.name, got)
	}
}

// A conversation in a track gets its own short context instead of the main chat's
// snapshot: nothing of Agents, computers, unread Lark chats, tasks or the knowledge base, at most 600 characters. The
// main conversation and a side conversation of the main space keep the snapshot.
func TestContextOfATrackConversationIsItsOwn(t *testing.T) {
	f := newFakeHub().withMachine()
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	f.setSessions("server", proto.Session{SessionID: "m1", MachineID: "server", Assistant: true},
		proto.Session{SessionID: "side", MachineID: "server", Assistant: true},
		proto.Session{SessionID: "health1", MachineID: "server", Assistant: true, Space: testSpace},
		proto.Session{SessionID: "plain1", MachineID: "server", Assistant: true, Space: "qa-plain"})
	s := newTestService(t, f)
	ctx := context.Background()
	text := func(id string) string {
		return s.Context(ctx, ContextParams{SessionID: id, MachineID: "server"}).Text
	}
	for id, head := range map[string]string{"health1": "**This is a conversation in the “QA private” space**", "plain1": "**This is a conversation in the “qa-plain” space**"} {
		got := text(id)
		if !strings.HasPrefix(got, head) || !strings.Contains(got, "\n- Now: ") || !strings.Contains(got, "- Answer in: Simplified Chinese") {
			t.Errorf("%s: not its track's context:\n%s", id, got)
		}
		if n := utf8.RuneCountInString(got); n > spaceContextMax {
			t.Errorf("%s: %d characters", id, n)
		}
		for _, leak := range []string{"Agents", "Computers online", "Feishu", "to-dos", "knowledge base", "Model profile", "iPhone", "main conversation"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: its context tells %q:\n%s", id, leak, got)
			}
		}
	}
	if got := text("m1"); !strings.HasPrefix(got, "**This is the main conversation**") || !strings.Contains(got, "- Agents: ") {
		t.Errorf("the main conversation lost its snapshot:\n%s", got)
	}
	if got := text("side"); !strings.Contains(got, "- Agents: ") || strings.Contains(got, "conversation in the") {
		t.Errorf("a side conversation of the main space lost its snapshot:\n%s", got)
	}
}
