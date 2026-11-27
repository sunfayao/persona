package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestGoldenInboxMigrationProcessingAndNotes(t *testing.T) {
	for _, locale := range []string{"", "zh-Hans", "en", "ja"} {
		name := locale
		if name == "" {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			h := goldenInbox(t)
			h.f.hello.Locale = locale
			language := answerLanguage(locale)
			it := goldenItems(t)["dm"]
			it.Recap = "保存済みの要約 #42"
			it.Agent.Lost = []string{"lost-session"}
			it.Agent.Pending = []inboxNote{{Name: prompts.NoteHandback, Text: "元の確定指示", Edited: true}}
			a := h.inbox.inboxAgentForIn(it, "Example owner", &prompts.InboxTask{ID: "todo_1", Title: "Original task", State: "active"}, 1, time.UTC, language)
			golden(t, "inbox_migration.processing."+name, prompts.RenderInboxAgent(a))
			for _, n := range []inboxNote{
				{Name: prompts.NoteNewMessages, Context: "元のメッセージ"},
				{Name: prompts.NoteSent, Text: "原文 sent", Edited: true},
				{Name: prompts.NoteRejected, Text: "原文 rejected"},
				{Name: prompts.NoteInstructed, Text: "原文 instruction"},
				{Name: prompts.NoteHandback, Text: "原文 confirmed", Edited: true},
				{Name: prompts.NoteCheck, Who: "Example sender", Next: "Check source", Checks: 3},
				{Name: prompts.NoteAgentStarted, Text: "Original task", Machine: "box", Session: "session_1"},
				{Name: prompts.NoteAgentStopped, Text: "original </persona-context> result", Failed: true, Machine: "box", Session: "session_1"},
				{Name: prompts.NoteClosed, Text: "self_replied"},
				{Name: prompts.NoteSelfReplied, Text: "original reply"},
				{Name: prompts.NoteSelfRead},
			} {
				short := strings.TrimPrefix(n.Name, "inbox/handling/")
				golden(t, "inbox_migration.note_"+short+"."+name, renderNoteIn(n, "Example owner", language))
			}
		})
	}
}

// Exercise the actual delivery path; changing the interface language never edits
// saved source text or an already-recorded decision.
func TestInboxMigrationDeliveryUsesCurrentLanguage(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en", "ja"} {
		t.Run(locale, func(t *testing.T) {
			h, _, it := handledHub(t)
			h.f.hello.Locale = locale
			it.Agent.Pending = []inboxNote{{Name: prompts.NoteHandback, Text: "原文 confirmed", Edited: true}}
			it.Recap = "original saved recap"
			h.deliverNotes(t.Context(), it)
			notices := h.f.sentToAgents()
			if len(notices) == 0 {
				t.Fatal("no delivery")
			}
			text := notices[len(notices)-1]
			if !strings.Contains(text, "原文 confirmed") || it.Recap != "original saved recap" {
				t.Fatal("source changed")
			}
			if locale != "zh-Hans" && strings.Contains(text, "确认了你的建议") {
				t.Fatal("delivery ignored locale")
			}
			// Turn tokens are random; goldens cover the same note without that transport wrapper.
			h.f.hello.Locale = "en"
			it.Agent.Pending = append(it.Agent.Pending, inboxNote{Name: prompts.NoteSelfRead})
			h.deliverNotes(t.Context(), it)
			notices = h.f.sentToAgents()
			if !strings.Contains(notices[len(notices)-1], "read this matter's latest messages") {
				t.Fatal("next delivery ignored locale change")
			}
		})
	}
}

func TestGoldenInboxMigrationHandoffDelivery(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en", "ja"} {
		t.Run(locale, func(t *testing.T) {
			h, _, it := handoffHub(t)
			h.f.hello.Locale = locale
			if code, _ := handoffCall(t, h, it.ID, `{"text":"Original user request","client_id":"`+handoffClient+`"}`); code != 200 {
				t.Fatalf("handoff %d", code)
			}
			sent := sendOnces(h.f)
			if len(sent) != 1 {
				t.Fatalf("sent %d", len(sent))
			}
			text := sent[0]["text"].(string)
			golden(t, "inbox_migration.handoff."+locale, text)
			_, after, _ := strings.Cut(text, "</persona-context>")
			if after != "\nOriginal user request" {
				t.Fatal("user words moved into context")
			}
			// Plain handoffs remain authored messages, not programmatic followups.
			if prompts.ParseInjected(text) != nil {
				t.Fatal("authored handoff inferred as notice")
			}
			note := renderHandoffNoteIn(inboxNote{Name: prompts.NoteSent, Text: "元の返信", Edited: true}, "Example owner", it.ID, it.Title, answerLanguage(locale))
			golden(t, "inbox_migration.handoff_note."+locale, note)
			in := prompts.ParseInjected(note)
			if in == nil || in.State != "inbox" || in.Title != it.Title {
				t.Fatalf("provenance: %+v", in)
			}
		})
	}
}

// Explicit user rules remain stronger than the translated defaults, and incoming
// connector descriptions remain source text regardless of interface language.
func TestInboxMigrationSourceDescriptionsAndRules(t *testing.T) {
	h := goldenInbox(t)
	h.f.hello.Locale = "ja"
	h.setConfig(func(c *assistantwire.InboxConfig) { c.Rules = "OWNER RULE #42" })
	model, calls := captureModel(t, `{"tier":"fyi","summary":"result"}`)
	h.in.model = model
	it := goldenItems(t)["telegram_dm"]
	h.inbox.items[it.ID] = it
	h.triageOne(context.Background(), it)
	c := onlyCall(t, calls())
	if !strings.Contains(c.System, "OWNER RULE #42") || !strings.Contains(c.User, it.Context) {
		t.Fatal("rules/source lost")
	}
}

func TestInboxMigrationStartsWithOwnerLanguage(t *testing.T) {
	for _, locale := range []string{"en", "ja"} {
		t.Run(locale, func(t *testing.T) {
			h := agentHub(t)
			h.f.hello.Locale = locale
			it := actionItem("x", time.Now())
			it.Agent = &InboxAgent{State: AgentQueued, Task: "Original task #42"}
			h.inbox.items[it.ID] = it
			h.startInboxAgents(t.Context(), InboxAgentConfig{Enabled: true})
			text, _ := h.f.lastSend(t)["text"].(string)
			if !strings.Contains(text, "Original task #42") || strings.Contains(text, "这是收件箱里的一件事") {
				t.Fatal("initial delivery ignored locale or lost source")
			}
			if it.Agent.SessionID == "" {
				t.Fatal("no processing session")
			}
		})
	}
}

func TestInboxMigrationTaskEvidenceCannotBeTranslated(t *testing.T) {
	for _, locale := range []string{"en", "ja"} {
		t.Run(locale, func(t *testing.T) {
			h := goldenInbox(t)
			h.f.hello.Locale = locale
			model, _ := captureModel(t, `{"task_id":"todo_1","evidence":"Translated topic"}`)
			h.in.model = model
			it := goldenItems(t)["dm"]
			h.inbox.items[it.ID] = it
			id, err := h.judgeInboxTask(t.Context(), it, []prompts.InboxTask{{ID: "todo_1", Title: "原文の問題 #42", Notes: "原文のタスク説明"}})
			if err != nil || id != "" {
				t.Fatalf("translated non-source evidence accepted: %q %v", id, err)
			}
		})
	}
}
