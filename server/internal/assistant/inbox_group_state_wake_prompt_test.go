package assistant

import (
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestInboxSelfRepliedPromptRestrictions(t *testing.T) {
	got := prompts.RenderInboxHandlingNote(prompts.NoteSelfReplied, prompts.InboxHandlingNote{
		Owner: "示例用户",
		Text:  "2026-11-13 14:30",
	})
	requireInboxWakePrompt(t, got,
		"call inbox_suggest with a conclusion (outcome done)",
		"give only recap and follow",
	)
}

func TestInboxSelfReadPromptRestrictions(t *testing.T) {
	got := prompts.RenderInboxHandlingNote(prompts.NoteSelfRead, prompts.InboxHandlingNote{Owner: "示例用户"})
	requireInboxWakePrompt(t, got,
		"call inbox_suggest with a conclusion",
		"submit only a suggestion, or recap and follow to keep waiting",
	)
}

func TestInboxCheckPromptRestrictions(t *testing.T) {
	got := prompts.RenderInboxHandlingNote(prompts.NoteCheck, prompts.InboxHandlingNote{
		Owner:  "示例用户",
		Who:    "张三",
		Next:   "等他给报价",
		Checks: 2,
	})
	requireInboxWakePrompt(t, got,
		"kind conclusion",
		"one suggestion",
		"give recap and follow",
	)
}

func requireInboxWakePrompt(t *testing.T, got string, outcomes ...string) {
	t.Helper()
	want := []string{
		"Do not start Agents",
		"send messages",
		"visible to others in this turn",
		"read-only",
		"the program executes suggestions only after 示例用户 confirms",
	}
	want = append(want, outcomes...)
	for _, part := range want {
		if !strings.Contains(got, part) {
			t.Errorf("prompt does not contain %q:\n%s", part, got)
		}
	}
}
