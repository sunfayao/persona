//go:build !windows

package node

import (
	"context"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// multiAsk is an AskUserQuestion of four questions, one of them multi-select, the shape Claude Code writes (made-up
// words, the keys in the order the desktop app's Claude Code writes them).
const multiAsk = `{"questions":[` +
	`{"header":"Source","multiSelect":false,"options":[{"description":"The app's own setting","label":"App"},{"description":"The account's","label":"Account"}],"question":"Which setting wins?"},` +
	`{"header":"Folding","multiSelect":false,"options":[{"description":"One line","label":"Fold"},{"description":"All of it","label":"Expand"}],"question":"How are long rows shown?"},` +
	`{"header":"Badges","multiSelect":true,"options":[{"label":"Unread"},{"label":"Mentions"},{"label":"Drafts"}],"question":"Which counts show?"},` +
	`{"header":"Alerts","multiSelect":false,"options":[{"label":"Always"},{"label":"Never"}],"question":"Alert on a mention?"}]}`

func askCall(id, toolUse, input string) string {
	return `{"type":"assistant","uuid":"a-` + toolUse + `","timestamp":"2026-11-04T09:00:09Z","sessionId":"` + id + `","message":{"role":"assistant",` +
		`"content":[{"type":"tool_use","id":"` + toolUse + `","name":"AskUserQuestion","input":` + input + `}]}}`
}

func toolResult(id, toolUse, text string) string {
	return `{"type":"user","uuid":"r-` + toolUse + `","timestamp":"2026-11-04T09:01:00Z","sessionId":"` + id + `","message":{"role":"user",` +
		`"content":[{"type":"tool_result","tool_use_id":"` + toolUse + `","content":"` + text + `"}]}}`
}

func TestOutsideAnswerWithTrailingTextUsesNow(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"choose"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T09:00:09Z","sessionId":"`+sidA+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_trailing","name":"AskUserQuestion","input":{"questions":[{"question":"Which one?","header":"Pick","options":[{"label":"A"},{"label":"B"}],"multiSelect":false}]}},{"type":"text","text":"Please choose one."}]}}`)
	inbox := newFakeInbox(t, p)
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "waiting", inbox, nil)
	cards := n.outsideAskCards()
	if len(cards) != 1 {
		t.Fatalf("pending question cards = %d, want 1", len(cards))
	}
	if err := n.codexAnswer(context.Background(), sidA, cards[0].ID, proto.Resolution{Answers: map[string]string{"Which one?": "B"}}); err != nil {
		t.Fatalf("answer returned error: %v", err)
	}
	priority, _ := inboxPriority(t, inbox, 0)
	if priority != peerNow {
		t.Fatalf("card exists and answer returned nil, but priority = %q; the answer is queued behind the open question", priority)
	}
}

// No card where the phone could not answer, or nothing is asked: a claude without an inbox (the computer answers), a
// permission prompt (the latest call is not a question), a question answered on the computer, a busy claude. Handed
// back to the computer, a card stays off the phone while the claude asks it.
func TestAnOutsideClaudesQuestionCardOnlyWhileAnswerable(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"tidy the inbox"}}`,
		askCall(sidA, "toolu_one", `{"questions":[{"question":"Which one?","header":"Pick","options":[{"label":"A"},{"label":"B"}],"multiSelect":false}]}`))
	inbox := newFakeInbox(t, p)

	writeOutsideClaude(t, home, sidA, "claude-desktop", "waiting", nil, nil) // no inbox
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("no inbox, yet a card: %+v", cards)
	}
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Approve || s.AttentionReason != proto.AttentionClaudeComputer {
		t.Fatalf("no inbox: %+v %q", s.Capabilities, s.AttentionReason)
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "busy", inbox, nil)
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("busy, yet a card: %+v", cards)
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "waiting", inbox, nil)
	cards := n.outsideAskCards()
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	id := cards[0].ID
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Dismiss: true}); err != nil {
		t.Fatal(err)
	}
	n.syncCodexCards(context.Background())
	if n.cx.sent != "" {
		t.Fatalf("handed back, yet listed: %q", n.cx.sent)
	}
	if len(inbox.users()) != 0 {
		t.Fatalf("a hand-back sent something: %v", inbox.users())
	}
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Cancel: true}); err == nil {
		t.Fatal("Skip went through")
	}
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Answers: map[string]string{"Which one?": "  "}}); err == nil {
		t.Fatal("an empty answer went through")
	}

	// Answered on the computer, then a permission prompt: no card for either.
	appendLines(t, p, toolResult(sidA, "toolu_one", "A"),
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-04T09:02:00Z","sessionId":"`+sidA+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_bash","name":"Bash","input":{"command":"ls"}}]}}`)
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("a permission prompt is a card: %+v", cards)
	}
	forgetMetas(n)
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Approve {
		t.Fatalf("a permission prompt can be approved from the phone: %+v", s.Capabilities)
	}
}
