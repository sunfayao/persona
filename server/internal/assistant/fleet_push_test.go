package assistant

import (
	"github.com/sunfayao/persona/server/internal/i18n"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The session lists show what the assistant's snapshot says: the todo each agent works for, the quiet ones, the task
// on an agent's card; SpawnFrom is not shown. A snapshot that changes them has the hub told the machine's list.
func TestSessionsShowTheTodoReport(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1"}, {SessionID: "s2"}, {SessionID: "s3", SpawnFrom: "todo_1@mac/s1"}})
	report := &TodoReport{Links: []TodoLink{
		{MachineID: "mac", SessionID: "s1", TodoID: "todo_1", Title: "修登录崩溃", Task: true},
		{MachineID: "mac", SessionID: "s2", TodoID: "todo_2", Title: "买牛奶"},
	}, Quiet: []SessionRef{{MachineID: "mac", SessionID: "s3"}}}
	n := len(ft.toldOf(assistantwire.EventFleetSessions))
	s.fleetTakeSnapshot(Snapshot{Todos: report})
	if got := len(ft.toldOf(assistantwire.EventFleetSessions)); got != n+1 {
		t.Fatalf("lists told %d, want 1", got-n)
	}
	s.fleetTakeSnapshot(Snapshot{Todos: report})
	if got := len(ft.toldOf(assistantwire.EventFleetSessions)); got != n+1 {
		t.Fatal("the same report told the list again")
	}
	s1, s2, s3 := listed(t, s, "mac", "s1"), listed(t, s, "mac", "s2"), listed(t, s, "mac", "s3")
	if s1.Todo == nil || s1.Todo.ID != "todo_1" || s1.Todo.Title != "修登录崩溃" || s1.Quiet {
		t.Fatalf("s1 = %+v quiet %v", s1.Todo, s1.Quiet)
	}
	if s2.Todo == nil || s2.Todo.ID != "todo_2" || s3.Todo != nil || !s3.Quiet || s3.SpawnFrom != "" {
		t.Fatalf("s2 %+v, s3 %+v quiet %v from %q", s2.Todo, s3.Todo, s3.Quiet, s3.SpawnFrom)
	}
	if got := s.withTask(proto.Interaction{MachineID: "mac", SessionID: "s2"}).TaskTitle; got != "" {
		t.Fatalf("a todo by hand named on the card: %q", got)
	}
}

// fastDone shortens the finished push's delay for a test.
func fastDone(ft *fleetTest) {
	ft.f.mu.Lock()
	ft.f.hello.Notify = assistantwire.NotifyTimings{DoneDelayMS: 50, LongTurnMS: 60_000}
	ft.f.mu.Unlock()
}

// The own conversation of a scheduled task that pushes only what its runs send (#2396) is known by the task list the
// snapshot has; other tasks' conversations, a task without one yet and the main conversation are not.
func TestQuietTaskConversation(t *testing.T) {
	s := newFleetTestNoStore(t).s
	if s.quietTaskConversation("own-q") {
		t.Fatal("nothing reported yet: no conversation is quiet")
	}
	s.fleetTakeSnapshot(Snapshot{Tasks: []TaskView{
		{ID: "fu_q", Push: assistantwire.PushAsked, RunMachineID: "server", RunSessionID: "own-q"},
		{ID: "fu_e", RunMachineID: "server", RunSessionID: "own-e"},
		{ID: "fu_new", Push: assistantwire.PushAsked}, // its first run has not started its conversation yet
	}})
	for session, want := range map[string]bool{"own-q": true, "own-e": false, "main-1": false, "": false} {
		if got := s.quietTaskConversation(session); got != want {
			t.Errorf("%q: %v, want %v", session, got, want)
		}
	}
}

// A quiet task's normal finish on Claude Code or a headless Codex is no push, but not once that conversation is the
// main one, nor a runtime's (its failures are not on the session), nor an error.
func TestQuietTaskFinish(t *testing.T) {
	s := newFleetTestNoStore(t).s
	tasks := []TaskView{{ID: "fu_q", Push: assistantwire.PushAsked, RunSessionID: "own-q"}}
	s.fleetTakeSnapshot(Snapshot{Tasks: tasks})
	done := proto.Session{SessionID: "own-q", Status: proto.StatusDone, LastText: "没有新邮件。"}
	if s.quietTaskFinish(done) {
		t.Fatal("the assistant's settings (its main conversation) not known yet: pushed as ever")
	}
	s.fleetTakeSnapshot(Snapshot{Tasks: tasks, Profile: &Profile{MachineID: "server", Main: "main-1"}})
	if !s.quietTaskFinish(done) {
		t.Fatal("a quiet task's normal finish is not pushed")
	}
	codex := done
	codex.Agent = "codex"
	if !s.quietTaskFinish(codex) {
		t.Fatal("a quiet task's normal finish on a headless Codex is not pushed (#2571 reports its failures)")
	}
	codexFailed := codex
	codexFailed.TurnError, codexFailed.TurnErrorKind = "unexpected status 503 Service Unavailable", proto.TurnErrorTransient
	runtime := done
	runtime.Agent = "dsh"
	cut := done
	cut.LastText = "API Error: 529 overloaded"
	other := done
	other.SessionID = "main-1"
	for name, sess := range map[string]proto.Session{"headless codex, failed turn": codexFailed, "a runtime": runtime, "cut off": cut,
		"another conversation": other} {
		if s.quietTaskFinish(sess) {
			t.Errorf("%s is pushed as ever", name)
		}
	}
	s.fleetTakeSnapshot(Snapshot{Tasks: tasks, Profile: &Profile{MachineID: "server", Main: "own-q"}})
	if s.quietTaskFinish(done) {
		t.Fatal("the main conversation is never quiet, even when it was the task's")
	}
}

// A health conversation's finished turn is pushed without its words or title; another's says them as before.
func TestHealthDonePushHidesContent(t *testing.T) {
	head, text := doneAlert(i18n.ZhHans, proto.Session{Title: "血压", Cwd: "/home/me/.persona/tracks/qa-private", LastText: "这周平均 150/95"})
	if strings.Contains(head, "血压") || strings.Contains(text, "150") || head != "Persona" {
		t.Fatalf("health done: %q %q", head, text)
	}
	if head, text := doneAlert(i18n.ZhHans, proto.Session{Title: "周报", Cwd: "/home/me/.persona/assistant", LastText: "写好了"}); head != "周报" || text != "写好了" {
		t.Fatalf("assistant's done: %q %q", head, text)
	}
}

// A task notice answered by a turn told elsewhere is used up by it: the user's quick turn right after goes by the
// device's setting, not pushed as a report (#4133).
func TestToldElsewhereTurnUsesUpItsReportMark(t *testing.T) {
	ft := newFleetTestNoStore(t)
	fastDone(ft)
	ft.s.fleetTakeSnapshot(Snapshot{Channels: &ChannelsReport{Turns: []ChannelTurn{{MachineID: "mac", SessionID: "c1"}}}})
	work := proto.Session{SessionID: "c1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p-c1", UpdatedAt: time.Now()}
	done := work
	done.Status, done.LastText = proto.StatusDone, "好了"
	ft.s.noteReportTurn("c1")
	ft.sessions("mac", work)
	ft.sessions("mac", done)
	tb := ft.s.turnsBook()
	for deadline := time.Now().Add(2 * time.Second); !tb.startOf("mac/c1").IsZero(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the turn told elsewhere is still tracked")
		}
	}
	ft.s.fleetTakeSnapshot(Snapshot{Channels: &ChannelsReport{Turns: []ChannelTurn{}}})
	work.UpdatedAt = time.Now()
	ft.sessions("mac", work)
	done.UpdatedAt, done.LastText = time.Now(), "又好了"
	ft.sessions("mac", done)
	got := ft.pushesOf(assistantwire.NotifyDone, 1, 2*time.Second)
	if len(got) != 1 || got[0].LongMS != 60_000 {
		t.Fatalf("the user's turn after it: %+v", got)
	}
}

// The main chat's push for a turn that wrote a long report, called agent_send and closed with a short line: the node
// puts the turn's answer from the report on in the row's LastText (node assistantAnswer), and the body is that, so the
// report is what the lock screen shows first.
func TestDoneAlertBodyStartsWithTheReport(t *testing.T) {
	row := "今天的进展：第一项已经合并，第二项在等 CI，第三项需要你决定是否上线。 已经发给它了。"
	_, text := doneAlert(i18n.ZhHans, proto.Session{Title: "主对话", Cwd: "/home/me/.persona/assistant", LastText: row})
	if !strings.HasPrefix(text, "今天的进展") || !strings.HasSuffix(text, "已经发给它了。") {
		t.Fatalf("body %q", text)
	}
}
