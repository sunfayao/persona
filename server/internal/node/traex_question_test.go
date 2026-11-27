package node

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/screen"
)

const traeScreenQuestion = `─────────────────────────────────────────────────
  Question 2/2 (1 unanswered)
  What color should the demo use?

    1. Blue   Use a cool accent.
    2. Amber  Use a warm accent.
  ❯ 3. Type your answer

  type to answer | tab or esc clear answer | enter submit all
`

func traeQuestionFixture(t *testing.T) (*Node, *fakeHerdr) {
	t.Helper()
	n, f, _ := herdrNode(t)
	traexHistoryFixture(t, n, nil)
	writeTraexOwner(t, n.home, threadMain, os.Getpid())
	cacheTraexOwnerPane(t, os.Getpid())
	n.agents = map[string]host.Agent{threadMain: {Agent: traexAgent, PaneID: traexOwnerPane, AgentStatus: proto.StatusBlocked}}
	f.reply("pane.process_info", foreground([2]string{"traecli", "traecli"}))
	old, confirm := codexStep, codexConfirm
	codexStep, codexConfirm = 10*time.Millisecond, time.Second
	t.Cleanup(func() { codexStep, codexConfirm = old, confirm })
	return n, f
}

func publishedTraeQuestion(t *testing.T, n *Node) *screen.Dialog {
	t.Helper()
	res, err := request(t, n, "session.screen", map[string]any{"session_id": threadMain, "lines": screen.DialogLines, "trae_question": true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res["trae_question"])
	var d *screen.Dialog
	if err := json.Unmarshal(b, &d); err != nil || d == nil || res["text"] != "" {
		t.Fatalf("no structured native question or raw history leaked: %+v (%v)", res, err)
	}
	return d
}

func TestTraeQuestionAdvancesThenSubmitsAndLeavesFollowUpExplicit(t *testing.T) {
	n, f := traeQuestionFixture(t)
	pane := traexOwnerPane
	first := strings.NewReplacer("2/2 (1 unanswered)", "1/2 (2 unanswered)", "submit all", "submit answer",
		"  ❯ 3.", "    3.", "    1.", "  ❯ 1.").Replace(traeScreenQuestion)
	f.setScreen(pane, first)
	taps := 0
	const answer = "A teal accent"
	const follow = "Continue the synthetic demo"
	f.after(func(c herdrCall) {
		switch c.Method {
		case "pane.send_keys":
			taps++
			if taps == 1 {
				f.setScreen(pane, traeScreenQuestion)
			} else {
				f.setScreen(pane, traeEmpty)
			}
		case "pane.send_text":
			switch str(c.Params["text"]) {
			case pasteStart + answer + pasteEnd:
				f.setScreen(pane, strings.Replace(traeScreenQuestion, "Type your answer", answer, 1))
			case pasteStart + follow + pasteEnd:
				f.setScreen(pane, "────────────────────\n❯ "+follow+"\n────────────────────")
			}
		}
	})
	if !parseTraeComposer(first).dialog || n.traexStatus(context.Background(), host.Agent{PaneID: pane, RawTitle: "⠸ working"}) != proto.StatusBlocked {
		t.Fatal("a visible native question did not take precedence over a running spinner")
	}
	if err := n.sendTraeTerminal(context.Background(), pane, follow, false); err == nil {
		t.Fatal("ordinary follow-up reached the question")
	}
	assertTrace(t, f.trace("pane.send_text", "pane.send_keys"), nil)
	d := publishedTraeQuestion(t, n)
	if len(d.Options) != 3 {
		t.Fatalf("first question lacks choices: %+v", d)
	}
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "1"}); err != nil {
		t.Fatal(err)
	}
	last := publishedTraeQuestion(t, n)
	if last.Rev == d.Rev || len(last.Options) != 3 || last.Options[2].Kind != screen.KindTypeSomething {
		t.Fatalf("native next page was not published: %+v", last)
	}
	_, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "1"})
	if code, _ := refusalOf(t, err); code != screen.PromptChanged {
		t.Fatalf("stale card was not refused: %v", err)
	}
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": last.Rev, "option": "3", "text": answer}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.send_keys"), []string{sendKeys(pane, "enter"), sendKeys(pane, "enter")})
	if err := n.sendTraeTerminal(context.Background(), pane, follow, false); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.send_keys"), []string{sendKeys(pane, "enter"), sendKeys(pane, "enter"), sendKeys(pane, "enter")})
}

// traeSingleQuestion is a live TRAE CLI page of one question (redacted): its rows are "1." "2." and "3. Other", the
// cursor on 1; with cursor, the page with the cursor on that row.
func traeSingleQuestion(t *testing.T, cursor string) string {
	t.Helper()
	b, err := os.ReadFile("../screen/testdata/traecli-question-single-127.txt")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(b), "  ❯ 1.", "    1.", 1)
	return strings.Replace(page, "    "+cursor+".", "  ❯ "+cursor+".", 1)
}

func TestTraeQuestionAnswersTheOnlyQuestionWithAnOption(t *testing.T) {
	n, f := traeQuestionFixture(t)
	pane := traexOwnerPane
	keys := func(c herdrCall) bool { return c.Method == "pane.send_keys" }
	f.screenSteps(pane, keys, traeSingleQuestion(t, "1"), traeSingleQuestion(t, "2"), traeEmpty)
	d := publishedTraeQuestion(t, n)
	if len(d.Options) != 3 || d.Options[2].Kind != screen.KindTypeSomething {
		t.Fatalf("the only question lacks its choices: %+v", d)
	}
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "2"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.send_keys", "pane.send_text"), []string{sendKeys(pane, "down"), sendKeys(pane, "enter")})
}

func TestTraeQuestionAnswersTheOnlyQuestionWithWrittenText(t *testing.T) {
	n, f := traeQuestionFixture(t)
	pane := traexOwnerPane
	const answer = "A synthetic answer"
	typed := strings.Replace(traeSingleQuestion(t, "3"), "Type your answer.", answer, 1)
	f.screenSteps(pane, func(c herdrCall) bool { return c.Method == "pane.send_keys" || c.Method == "pane.send_text" },
		traeSingleQuestion(t, "1"), traeSingleQuestion(t, "2"), traeSingleQuestion(t, "3"), typed, traeEmpty)
	d := publishedTraeQuestion(t, n)
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "3", "text": answer}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.send_keys", "pane.send_text"), []string{sendKeys(pane, "down"), sendKeys(pane, "down"),
		call("pane.send_text", map[string]any{"pane_id": pane, "text": pasteStart + answer + pasteEnd}), sendKeys(pane, "enter")})
}

func TestTraeQuestionLeavesAmbiguousOrOwnedDraftInTerminal(t *testing.T) {
	for _, visible := range []string{
		strings.Replace(traeScreenQuestion, "(1 unanswered)", "(2 unanswered)", 1),
		strings.Replace(traeScreenQuestion, "Type your answer", "A native draft", 1),
	} {
		n, f := traeQuestionFixture(t)
		f.setScreen(traexOwnerPane, visible)
		d := publishedTraeQuestion(t, n)
		if len(d.Options) != 0 {
			t.Fatalf("unsafe choices were published: %+v", d)
		}
		_, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "1"})
		if code, _ := refusalOf(t, err); code != screen.DialogOpen {
			t.Fatalf("unsafe answer was accepted: %v", err)
		}
		assertTrace(t, f.trace("pane.send_text", "pane.send_keys"), nil)
	}
}

func TestTraeQuestionMovesFromFreeTextWithoutTypingAnOptionNumber(t *testing.T) {
	n, f := traeQuestionFixture(t)
	pane := traexOwnerPane
	f.setScreen(pane, traeScreenQuestion)
	d := publishedTraeQuestion(t, n)
	count := 0
	f.after(func(c herdrCall) {
		if c.Method != "pane.send_keys" {
			return
		}
		count++
		if count > 2 {
			f.setScreen(pane, traeEmpty)
			return
		}
		nextRow := []string{"    2.", "    1."}[count-1]
		next := strings.NewReplacer("  ❯ 3.", "    3.", nextRow, "  ❯"+nextRow[3:]).Replace(traeScreenQuestion)
		f.setScreen(pane, next)
	})
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": threadMain, "rev": d.Rev, "option": "1"}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace("pane.send_keys"), []string{sendKeys(pane, "up"), sendKeys(pane, "up"), sendKeys(pane, "enter")})
	assertTrace(t, f.trace("pane.send_text"), nil)
}
