package assistant

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// supAgent is the Agent the tests drive: its id is long enough for agent_supervise to name it.
const supAgent = "s1agent0"

// supAgentSession is agentSession (feishu_agent_done_test.go) as supAgent.
func supAgentSession() proto.Session {
	sess := agentSession()
	sess.SessionID = supAgent
	return sess
}

// supervisedEnv is a manager whose assistant runs on "server" with the main conversation conv1, the leader conversation
// lead1, the side conversation side1 and the background supervisor bg1 (on unless background is false: then the main
// conversation follows its own tasks up). Tasks are not quiet (main_tasks_quiet off), so nothing but supervision keeps
// a finished turn from the pushes. The Lark bot is paired with its pushes switch on, and the Agent supAgent is listed on mac.
// logs keeps what the assistant logged.
type supervisedEnv struct {
	*fleetTest
	mu   sync.Mutex
	logs []string
}

// task is a to-do opened from conversation origin with the Agent supAgent on it, its ledger naming supervisor ("": none), as
// the start of a planned task writes it (tasks_start.go: superviseTodo, then the Agent linked).
func (e *supervisedEnv) task(origin, supervisor string) Todo {
	e.t.Helper()
	ctx := context.Background()
	todo, err := e.s.createTodo(ctx, "修登录崩溃", "iPad 横屏时登录页崩溃", origin, "")
	if err != nil {
		e.t.Fatal(err)
	}
	if supervisor != "" {
		e.s.superviseTodo(ctx, todo.ID, supervisor, []string{"PR 合并到 main"}, 30)
	}
	if err := e.s.linkTodoAgent(ctx, todo.ID, "mac", supAgent, "修登录", "/code/app", "claude"); err != nil {
		e.t.Fatal(err)
	}
	return todoNow(e.s, todo.ID)
}

// agentDoneCards are the Lark cards posted for an Agent's finished turn (they carry the reply hint, #5438).
func (e *supervisedEnv) agentDoneCards() []map[string]any {
	hint := i18n.T("", "assistant-feishu_cards.agent_done_reply_hint")
	var out []map[string]any
	for _, p := range e.f.appParams(AppFeishu, FeishuPost) {
		if card, _ := p["card"].(string); strings.Contains(card, hint) {
			out = append(out, p)
		}
	}
	return out
}

// holdDone puts the finished turn's delay out of the test's reach (an hour), so finishNow decides it instead.
func (e *supervisedEnv) holdDone() {
	e.f.mu.Lock()
	e.f.hello.Notify.DoneDelayMS = 3_600_000
	e.f.mu.Unlock()
}

// finishNow runs the delayed decision on supAgent's latest turn end now (notifyFinishedNow), as its timer would, so the
// check that follows does not depend on the delay's timer firing in time.
func (e *supervisedEnv) finishNow() {
	e.t.Helper()
	book := e.s.turnsBook()
	gen := 0
	waitFor(e.t, "the turn's end tracked", func() bool {
		book.mu.Lock()
		defer book.mu.Unlock()
		tr := book.turns["mac/"+supAgent]
		if tr == nil {
			return false
		}
		gen = tr.gen
		return true
	})
	e.s.notifyFinishedNow("mac", supAgent, gen, e.s.notifyTimings())
	// A push and a Feishu card the decision made go out on goroutines of their own (deliver, feishuAgentDone), and
	// nothing marks one that was never started: give them a moment to land before a check that none did.
	time.Sleep(100 * time.Millisecond) // sleep-ok: a negative check, no event marks a push or card that was not sent
}

// donePushes are the phone's "finished" pushes for the Agent supAgent.
func (e *supervisedEnv) donePushes() []Notification {
	var out []Notification
	for _, p := range e.pushesOf(assistantwire.NotifyDone, 0, 0) {
		if p.SessionID == supAgent {
			out = append(out, p)
		}
	}
	return out
}

// oneDonePush checks that supAgent's finished turn was pushed to the phone once. deliver sends the push on a goroutine
// of its own, apart from the Lark card's (feishuAgentDone), so a card that has landed says nothing of the push: wait
// for the push as for the card, then count (#8684).
func (e *supervisedEnv) oneDonePush(t *testing.T) {
	t.Helper()
	waitFor(t, "the finished push", func() bool { return len(e.donePushes()) >= 1 })
	if got := e.donePushes(); len(got) != 1 {
		t.Fatalf("pushed %d finished pushes, want 1: %+v", len(got), got)
	}
}

// logged are the lines logged so far that contain all of want.
func (e *supervisedEnv) logged(want ...string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
next:
	for _, l := range e.logs {
		for _, w := range want {
			if !strings.Contains(l, w) {
				continue next
			}
		}
		out = append(out, l)
	}
	return out
}
