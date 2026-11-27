package voice

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Cards by voice: a question is asked a question at a time by its options' names, what the user
// says while it is asked is its answer, and the card is resolved; other cards are only said by their kind.

// dbQuestion is a question card in conversation sid: which database, Postgres or SQLite (each with what it means).
func dbQuestion(id, sid string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "m", SessionID: sid, Kind: proto.IxQuestion, ToolName: "AskUserQuestion",
		Questions: []proto.Question{{Question: "数据库用哪个？", Header: "数据库", Options: []proto.QuestionOption{
			{Label: "Postgres", Description: "生产环境常用"}, {Label: "SQLite", Description: "单个文件，适合本地"}}}}}
}

// askFrom matches the app's ask frame for card in state (by: who answered, "" any).
func askFrom(card, state, by string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		return f["type"] == "ask" && f["interaction_id"] == card && f["state"] == state && (by == "" || f["by"] == by)
	}
}

// saying matches what the extension says by itself with text in it.
func saying(text string) (string, func(map[string]any) bool) {
	return "said: " + text, func(f map[string]any) bool { return said(f) && strings.Contains(content(f), text) }
}

// A question that comes up in the bound conversation is asked by voice: the options by their names (not what they mean
// read out, not the typing hints of a text channel), the app told which card; "the second one" picks SQLite, the card is
// resolved through the hub, nothing is sent to the conversation, and the model is told on that delegation.
func TestAskAQuestionByVoice(t *testing.T) {
	hub := agentHub(t)
	hub.sessions["a1"].status = "working"
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	hub.subscribed("a1") // the call follows its conversation, run or not
	hub.raise(dbQuestion("q1", "a1"))
	asked := content(up.nextMatch(saying("数据库用哪个")))
	for _, want := range []string{"Claude Code has stopped to wait for the user to answer a question", "not finished", "Question (数据库): 数据库用哪个？", "Option 1: Postgres (what it means: 生产环境常用)",
		"Option 2: SQLite", "Read out only the options' names", "starts with \"Answer:\"", "(\"Stop:\")"} {
		if !strings.Contains(asked, want) {
			t.Errorf("asked without %q: %s", want, asked)
		}
	}
	if strings.Contains(asked, "reply with the number") {
		t.Errorf("asked as a text channel does: %s", asked)
	}
	a.until(askFrom("q1", askAsking, ""))

	a.send(map[string]any{"type": "text", "text": "第二个"})
	// The card was not asked of a turn the call handed over, but the call follows its bound conversation (observed
	// turns): what it does next is told by itself.
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "The user's answer went to Claude Code") || !strings.Contains(got, "not finished") ||
		strings.Contains(got, "What it does next is not followed in this call") {
		t.Fatalf("told: %s", got)
	}
	a.until(askFrom("q1", askAnswered, byVoice))
	res := hub.resolutions()
	if len(res) != 1 || res[0].ID != "q1" || res[0].Res.Answers["数据库用哪个？"] != "SQLite" {
		t.Fatalf("resolved %+v", res)
	}
	if msgs := hub.messages(); len(msgs) != 0 {
		t.Fatalf("sent %+v", msgs)
	}
}

// A card of several questions is asked one at a time: an answer not among the options is the answer itself (the "answer:" prefix
// taken off), the next question is told on that answer's delegation, a multi-select takes several names, and the card
// is resolved with every answer once the last is in.
func TestAskQuestionsOneAtATime(t *testing.T) {
	hub := agentHub(t)
	hub.cards = []proto.Interaction{{ID: "q2", MachineID: "m", SessionID: "a1", Kind: proto.IxQuestion, Questions: []proto.Question{
		{Question: "数据库用哪个？", Options: []proto.QuestionOption{{Label: "Postgres"}, {Label: "SQLite"}}},
		{Question: "要哪些功能？", MultiSelect: true, Options: []proto.QuestionOption{{Label: "登录"}, {Label: "搜索"}, {Label: "导出"}}},
	}}}
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	if got := content(up.nextMatch(saying("数据库用哪个"))); !strings.Contains(got, "question 1 of 2") {
		t.Fatalf("asked: %s", got)
	}
	a.until(askFrom("q2", askAsking, ""))
	a.send(map[string]any{"type": "text", "text": "回答：用 MySQL 吧"})
	next := content(up.nextMatch(on("dlg1")))
	for _, want := range []string{"question 2 of 2", "要哪些功能？", "Option 3: 导出", "say that several can be picked"} {
		if !strings.Contains(next, want) {
			t.Errorf("next question without %q: %s", want, next)
		}
	}
	if len(hub.resolutions()) != 0 {
		t.Fatal("resolved before its last question")
	}
	a.send(map[string]any{"type": "text", "text": "登录和导出"})
	if got := content(up.nextMatch(on("dlg2"))); !strings.Contains(got, "The user's answer went to") {
		t.Fatalf("told: %s", got)
	}
	res := hub.resolutions()
	if len(res) != 1 || res[0].Res.Answers["数据库用哪个？"] != "用 MySQL 吧" || res[0].Res.Answers["要哪些功能？"] != "登录, 导出" {
		t.Fatalf("resolved %+v", res)
	}
}

// A bare "answer:" while the question is asked (the model did not copy the words) is the user's caption: "the second one".
func TestAskBareAnswerMarkTakesTheCaption(t *testing.T) {
	hub := agentHub(t)
	hub.cards = []proto.Interaction{dbQuestion("q1", "a1")}
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		case "session.commentary.append":
			if f["delegation_id"] == nil && f["event_id"] == nil && content(f) == "第二个" {
				delegate(u, c, "dlg1", "回答：")
			}
		}
	})
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	up.nextMatch(saying("数据库用哪个"))
	a.until(askFrom("q1", askAsking, ""))
	a.send(map[string]any{"type": "text", "text": "第二个"})
	if got := content(up.nextMatch(on("dlg1"))); !strings.Contains(got, "The user's answer went to") {
		t.Fatalf("told: %s", got)
	}
	if res := hub.resolutions(); len(res) != 1 || res[0].Res.Answers["数据库用哪个？"] != "SQLite" {
		t.Fatalf("resolved %+v", res)
	}
}

// A new upstream session after a drop is asked the question again: the last one heard it, this one did not.
func TestAskAgainAfterAReconnect(t *testing.T) {
	hub := agentHub(t)
	hub.cards = []proto.Interaction{dbQuestion("q1", "a1")}
	var sessions atomic.Int32
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			sessions.Add(1)
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		case "session.commentary.append":
			if said(f) && strings.Contains(content(f), "数据库用哪个") && sessions.Load() == 1 {
				_ = c.Close(websocket.StatusGoingAway, "restart")
			}
		}
	})
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	up.nextMatch(saying("数据库用哪个"))
	a.until(func(f map[string]any) bool { return f["type"] == "link" && f["state"] == "live" })
	up.nextMatch(saying("数据库用哪个"))
	if n := sessions.Load(); n != 2 {
		t.Fatalf("%d sessions", n)
	}
}

// The call follows its bound conversation from start to end; the hub link drops (a redeploy) and connects again: it
// subscribes again from the last line it saw, not from where the call began (that would replay every turn's end since
// as if each had just ended).
func TestBoundCatchesUpFromTheLinesSeen(t *testing.T) {
	setVar(t, &linkBackoff, []time.Duration{50 * time.Millisecond})
	hub := agentHub(t)
	hub.add("a1", proto.Item{Role: "user", Kind: proto.KindText, Text: "修登录页的测试"}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "好"})
	up := boundUpstream(t, false)
	pl := testPlugin(t, liveProvider(up), hub.server(t))
	a := dialApp(t, pl, boundStart("a1", nil))
	a.until(ofType("ready"))
	hub.subscribed("a1")
	if _, sub := hub.subscribes("a1"); sub["since_seq"] != float64(2) {
		t.Fatalf("subscribed with %v (its last line is 2)", sub)
	}
	hub.add("a1", proto.Item{Role: "assistant", Kind: proto.KindText, Text: "第一轮"})
	hub.end("a1") // seq 4
	time.Sleep(200 * time.Millisecond)
	hub.dropWS()
	waitFor(t, "a second subscribe", func() bool { n, _ := hub.subscribes("a1"); return n >= 2 })
	if _, sub := hub.subscribes("a1"); sub["since_seq"] != float64(4) {
		t.Fatalf("subscribed again with %v (the link saw up to 4)", sub)
	}
}
