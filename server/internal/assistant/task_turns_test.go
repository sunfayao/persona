package assistant

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// turnPage is what the node pages as an agent's latest turn: the prompt, what the agent did since and, once the
// runtime closed the turn, its end as the transcript reader maps it (Codex's task_complete, transcript/codex.go).
// An open turn ends on a tool call still running.
func turnPage(turn, reply string, closed bool) []proto.Item {
	at := time.Now().UTC()
	items := []proto.Item{{ID: turn + "-ask#0", Kind: proto.KindText, Role: "user", Text: "iPad 横屏时登录页崩溃，修好并补测试", TS: at}}
	if reply != "" {
		items = append(items, proto.Item{ID: turn + "-reply#0", Kind: proto.KindText, Role: "assistant", Text: reply, TS: at})
	}
	if !closed {
		return append(items, proto.Item{ID: turn + "-tool#0", Kind: proto.KindToolUse, Role: "assistant", ToolName: "shell", ToolUseID: turn + "-call", TS: at})
	}
	return append(items, proto.Item{ID: turn + "-end#0", Kind: proto.KindTurnDuration, Role: "system", TS: at,
		TurnDuration: &proto.TurnDuration{DurationMS: 1200, Source: "codex_task_complete", TurnID: turn}})
}

// turnRow is the task's agent s1 on box as its node lists it: status, the transcript's size rev and its last words.
func turnRow(status string, rev int64, text string) proto.Session {
	state := proto.StateUnread
	switch status {
	case proto.StatusWorking:
		state = proto.StateWorking
	case proto.StatusIdle:
		state = proto.StateIdle
	case proto.StatusUnknown:
		state = proto.StateUnknown
	}
	return proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "codex", Status: status, State: state,
		LastText: text, Rev: rev, UpdatedAt: time.Now()}
}

// turnCounts are what completion handling did for a task: the receipts in its conversation, the done judge's calls
// to the model, and the supervision messages put into the main conversation.
type turnCounts struct{ receipts, judgments, messages int }

var oneEach = turnCounts{1, 1, 1}

// sweeps runs the supervision sweep n times, each after the looks of the one before finished.
func sweeps(t *testing.T, s *Service, todo string, n int) {
	t.Helper()
	for range n {
		s.superviseWatch(context.Background(), time.Now())
		turnsQuiet(t, s, todo)
	}
}

// itemReadsCounted counts the session.items reads from here on.
func itemReadsCounted(f *fakeHub) *atomic.Int32 {
	var n atomic.Int32
	f.hookReads(receiptHooks{before: func(int) { n.Add(1) }})
	return &n
}

// shortTurnRetries has the sweep's looks that could not finish tried again at once, and puts the spacing back.
func shortTurnRetries(t *testing.T, base time.Duration) {
	t.Helper()
	old := turnRetryBase
	turnRetryBase = base
	t.Cleanup(func() { turnRetryBase = old })
}

// restartedOn is the manager started again on database st: newQuietServiceOn without setting the package's timings a
// second time. The test's first manager is not stopped the way a process is, and what it still runs reads them.
func restartedOn(t *testing.T, st Store) (*Service, *fakeHub) {
	t.Helper()
	s, f := newTaskServiceOn(t, st)
	shortSupervise(t, s)
	f.hello.Assistant.MachineID = "server"
	setSettings(t, s, func(v *Settings) { v.Main = "conv1" })
	f.known["conv1"] = true
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle})
	return s, f
}

// turnsQuiet waits until manager s is in the middle of nothing for task todo: no turn end being settled or judged, no
// sweep's look going. A restart is taken from there, so that the two managers of a test never work at once.
func turnsQuiet(t *testing.T, s *Service, todo string) {
	t.Helper()
	waitFor(t, "the manager to be done with the turn", func() bool {
		b := &s.tasks.turns
		b.mu.Lock()
		defer b.mu.Unlock()
		return !b.settling(todo) && !b.recovering[todo]
	})
}

// A snapshot taken before Inbox I/O is not permission to rewrite or queue a newer turn's signal.
func TestCompletionReplayRejectsStaleLedger(t *testing.T) {
	for _, change := range []string{"machine", "session", "native turn", "judgment", "told", "missing", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			s, _ := newQuietService(t)
			s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
			old := taskTurn{Machine: "box", Session: "session", Key: "turn-a", Seen: time.Now(),
				Signal: &turnSignal{Kind: sigUnjudged, Key: "signal-a", Machine: "box", Detail: "Needs review"}}
			current := old
			sig := *old.Signal
			current.Signal = &sig
			switch change {
			case "machine":
				current.Machine = "other-box"
			case "session":
				current.Session = "other-session"
			case "native turn":
				current.Key = "turn-b"
			case "judgment":
				current.Signal.Key = "signal-b"
			case "told":
				now := time.Now()
				current.Signal.Told = &now
			}
			b := &s.tasks.turns
			b.mu.Lock()
			if !s.turnsLocked(context.Background()) {
				t.Fatal("load ledger")
			}
			if change != "missing" {
				b.doc.Turns["task"] = current
			}
			b.mu.Unlock()
			s.replayTurnSignal("task", old)
			b.mu.Lock()
			got, exists := b.doc.Turns["task"]
			b.mu.Unlock()
			if change == "missing" {
				if exists {
					t.Fatal("replay created a ledger entry")
				}
			} else if got.Key != current.Key || got.Machine != current.Machine || got.Session != current.Session || *got.Signal != *current.Signal {
				t.Fatalf("replay changed the ledger: %+v", got)
			}
			s.supervise.mu.Lock()
			n := len(s.supervise.pending)
			s.supervise.mu.Unlock()
			want := 0
			if change == "unchanged" {
				want = 1
			}
			if n != want {
				t.Fatalf("queued %d signals, want %d", n, want)
			}
		})
	}
}

// What the sweep's look costs: a transcript read only for a supervised task whose agents look stopped, at most
// turnRecoverMost going at a time, one for each task until its row moves again, and none for a task whose agent
// works. So many stopped sessions cost nothing by themselves: only open supervised tasks are looked at, once each.
func TestTurnSweepReadsAreBounded(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	tasks := 2*turnRecoverMost + 1
	var rows []proto.Session
	for i := range tasks + 1 {
		sid := fmt.Sprintf("agent%02d", i)
		todo, err := s.createTodo(ctx, "任务 "+sid, "做 "+sid, "conv1", "")
		if err != nil {
			t.Fatal(err)
		}
		s.superviseTodo(ctx, todo.ID, "conv1", nil, 0)
		if err := s.linkTodoAgent(ctx, todo.ID, "box", sid, "任务", "/code/app", "codex"); err != nil {
			t.Fatal(err)
		}
		row := proto.Session{SessionID: sid, MachineID: "box", Cwd: "/code/app", Agent: "codex", Status: proto.StatusIdle,
			State: proto.StateIdle, Rev: 7, UpdatedAt: time.Now()}
		if i == tasks { // one more whose agent is listed as working: never read
			row.Status, row.State = proto.StatusWorking, proto.StateWorking
		}
		rows = append(rows, row)
	}
	for i := range 20 { // and stopped sessions of no task at all
		rows = append(rows, proto.Session{SessionID: fmt.Sprintf("other%02d", i), MachineID: "box", Cwd: "/code/app", Agent: "codex",
			Status: proto.StatusDone, State: proto.StateUnread, Rev: 3, UpdatedAt: time.Now()})
	}
	f.setSessions("box", rows...)
	f.setItems(turnPage("turn-1", "", false)...) // every transcript: a turn still open
	var reads, going, most atomic.Int32
	release := make(chan struct{})
	f.hookReads(receiptHooks{before: func(int) {
		reads.Add(1)
		if n := going.Add(1); n > most.Load() {
			most.Store(n)
		}
		<-release
	}, after: func(int) { going.Add(-1) }})
	s.superviseWatch(ctx, time.Now())
	waitFor(t, "the first reads", func() bool { return int(going.Load()) == turnRecoverMost })
	s.superviseWatch(ctx, time.Now()) // a tick while they are still going starts no more
	quiet := time.NewTicker(50 * time.Millisecond)
	defer quiet.Stop()
	<-quiet.C // a tick this soon starts no further reads while the first batch is held
	if got := int(reads.Load()); got != turnRecoverMost {
		t.Fatalf("%d reads going at a time, want at most %d", got, turnRecoverMost)
	}
	close(release)
	waitFor(t, "a read for each task", func() bool {
		s.superviseWatch(ctx, time.Now())
		return int(reads.Load()) == tasks && going.Load() == 0
	})
	for range 3 {
		s.superviseWatch(ctx, time.Now())
	}
	<-quiet.C // rows that did not move schedule no further reads
	if got := int(reads.Load()); got != tasks || int(most.Load()) > turnRecoverMost {
		t.Fatalf("%d reads for %d tasks whose rows did not move (at most %d at a time, want %d)", got, tasks, most.Load(), turnRecoverMost)
	}
	rows[0].Rev, rows[0].UpdatedAt = 8, time.Now() // one transcript grew
	f.setSessions("box", rows...)
	waitFor(t, "the read of the row that moved", func() bool {
		s.superviseWatch(ctx, time.Now())
		return int(reads.Load()) == tasks+1
	})
	s.superviseWatch(ctx, time.Now())
	<-quiet.C // the row that did not move again schedules nothing
	if got := int(reads.Load()); got != tasks+1 {
		t.Fatalf("%d reads after one row moved, want %d", got, tasks+1)
	}
}

// The stretch's receipt is reserved before the agents are asked once more whether they stopped. A node that does not
// answer that one time (a dropped link) must not leave the reservation behind: the turn still gets its receipt and
// its judgment once the node answers again, when its end is told again and, when nothing tells of it again, by the
// look the manager takes by itself a little later.
func TestTaskReceiptReservationIsReleased(t *testing.T) {
	for _, how := range []string{"told again", "by itself"} {
		t.Run(how, func(t *testing.T) {
			reachA, reachB, reachM, retry := agentReachAttempts, agentReachBackoff, agentReachMax, receiptRetryAfter
			agentReachAttempts, agentReachBackoff, agentReachMax, receiptRetryAfter = 2, time.Millisecond, 200*time.Millisecond, time.Hour
			if how == "by itself" {
				receiptRetryAfter = 30 * time.Millisecond
			}
			t.Cleanup(func() {
				agentReachAttempts, agentReachBackoff, agentReachMax, receiptRetryAfter = reachA, reachB, reachM, retry
			})
			s, f := newTaskService(t) // no main conversation: the task is not supervised, so no sweep looks at it
			f.judge = `{"verdict":"done","reason":"修好了"}`
			todo := startedTask(t, s, f)
			if todo.Supervision != nil {
				t.Fatalf("supervised: %+v", todo.Supervision)
			}
			ctx := context.Background()
			agentTurn(s, f, proto.StatusWorking, "", 0)
			f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusDone,
				State: proto.StateUnread, LastText: "修好了", UpdatedAt: time.Now()})
			f.mu.Lock()
			f.onDevicesList = func() { // the node stops answering between the first look and the second
				f.mu.Lock()
				f.sessionsFail["box"] = agentReachAttempts
				f.mu.Unlock()
			}
			f.mu.Unlock()
			s.taskTurnEnded(ctx, "box", "s1")
			f.mu.Lock()
			fails, hook := f.sessionsFail["box"], f.onDevicesList
			f.mu.Unlock()
			if hook != nil || fails != 0 {
				t.Fatalf("the second look did not meet the dropped link: hook left %v, %d failures left", hook != nil, fails)
			}
			if how == "told again" {
				if got := len(recoveryReceipts(t, s, todo.ID)); got != 0 {
					t.Fatalf("%d receipts while the node did not answer", got)
				}
				s.taskTurnEnded(ctx, "box", "s1") // the node answers again, and the same turn end is told again
			}
			waitFor(t, "the receipt", func() bool { return len(recoveryReceipts(t, s, todo.ID)) == 1 })
			waitFor(t, "judge-done delivery to stay open", func() bool {
				current := todoNow(s, todo.ID)
				return current.DoneAt == nil && current.Check != nil && current.Check.Verdict == verdictDone
			})
			quiet := time.NewTicker(100 * time.Millisecond)
			defer quiet.Stop()
			<-quiet.C // a second receipt or judgment would show by now
			f.mu.Lock()
			judged := f.judged
			f.mu.Unlock()
			if got := len(recoveryReceipts(t, s, todo.ID)); got != 1 || judged != 1 {
				t.Fatalf("%d receipts, judged %d times", got, judged)
			}
		})
	}
}

// A turn is closed where the runtime says so, and is named by that end; whatever starts or continues a turn after it
// leaves no closed turn to go by.
func TestTurnEndOf(t *testing.T) {
	ask := proto.Item{ID: "u1#0", Kind: proto.KindText, Role: "user", Text: "修登录崩溃"}
	reply := proto.Item{ID: "a1#0", Kind: proto.KindText, Role: "assistant", Text: "修好了"}
	codexEnd := proto.Item{ID: "line9#0", Kind: proto.KindTurnDuration, Role: "system", TurnDuration: &proto.TurnDuration{DurationMS: 5, Source: "codex_task_complete", TurnID: "turn-7"}}
	claudeEnd := proto.Item{ID: "d1#0", Kind: proto.KindTurnDuration, Role: "system", TurnDuration: &proto.TurnDuration{DurationMS: 5, Source: "claude", ParentUUID: "a1"}}
	stopped := proto.Item{ID: "n1#0", Kind: proto.KindNotice, Role: "system", Text: userStoppedNotice}
	for _, c := range []struct {
		name  string
		items []proto.Item
		key   string
	}{
		{"nothing read", nil, ""},
		{"open: the reply is last", []proto.Item{ask, reply}, ""},
		{"open: a tool runs", []proto.Item{ask, reply, {ID: "t1#0", Kind: proto.KindToolUse, Role: "assistant", ToolUseID: "c"}}, ""},
		{"Codex task_complete: its turn id", []proto.Item{ask, reply, codexEnd}, "turn-7"},
		{"Claude Code turn_duration: its item", []proto.Item{ask, reply, claudeEnd}, "d1#0"},
		{"stopped by the user", []proto.Item{ask, stopped}, "n1#0"},
		{"an end with nothing to name it", []proto.Item{ask, reply, {Kind: proto.KindTurnDuration, Role: "system"}}, ""},
		{"a compaction notice after the end", []proto.Item{ask, reply, codexEnd, {ID: "n2#0", Kind: proto.KindNotice, Role: "system", Text: "compacted", Compact: &proto.Compact{}}}, "turn-7"},
		{"a queued message started the next turn", []proto.Item{ask, reply, codexEnd, {ID: "u2#0", Kind: proto.KindText, Role: "user", Text: "再看看深色", Queued: true}}, ""},
		{"a wake started the next turn", []proto.Item{ask, reply, claudeEnd, {ID: "w1#0", Kind: proto.KindNotice, Role: "system", Wake: true}}, ""},
		{"a background task's notice started the next turn", []proto.Item{ask, reply, claudeEnd, {ID: "k1#0", Kind: proto.KindNotice, Role: "system", Task: &proto.TaskNote{Kind: "shell"}}}, ""},
		{"the next turn ended too", []proto.Item{ask, reply, codexEnd, {ID: "u2#0", Kind: proto.KindText, Role: "user", Text: "再看看深色", Queued: true},
			{ID: "line20#0", Kind: proto.KindTurnDuration, Role: "system", TurnDuration: &proto.TurnDuration{DurationMS: 5, Source: "codex_task_complete", TurnID: "turn-8"}}}, "turn-8"},
	} {
		end, closed := turnEndOf(c.items)
		if closed != (c.key != "") || end.Key != c.key {
			t.Errorf("%s: closed %v with key %q, want %q", c.name, closed, end.Key, c.key)
		}
	}
	if a, b := receiptID("todo1", taskTurn{Machine: "box", Session: "s1", Key: "turn-7"}), receiptID("todo1", taskTurn{Machine: "box", Session: "s1", Key: "turn-8"}); a == b ||
		a != receiptID("todo1", taskTurn{Machine: "box", Session: "s1", Key: "turn-7", Rev: 9}) || receiptID("todo1", taskTurn{Machine: "box", Session: "s1"}) != "" {
		t.Errorf("receipt ids: %q %q", a, b)
	}
}

// With the fleet the manager's own, the rows of a computer that is away are its copy of the node's last list, marked
// unknown: the sweep asks that computer nothing, tick after tick, and looks once it is back.
func TestTurnSweepLeavesAComputerThatIsAwayAlone(t *testing.T) {
	ft := newFleetTestNoStore(t)
	node := ft.d.add("box", "Box")
	away := func(is bool) {
		ft.d.mu.Lock()
		node.away = is
		ft.d.mu.Unlock()
	}
	linked := time.Now().Add(-time.Hour)
	task := Todo{ID: "todo_away", Origin: "conv1", CreatedAt: linked, Supervision: &Supervision{Supervisor: "conv1"},
		Agents: []TodoAgent{{MachineID: "box", SessionID: "s1", LinkedAt: linked}}}
	row := turnRow(proto.StatusUnknown, 3, "修好了") // as its rows are kept when its connection ends
	ft.f.setSessions("box", row)
	ft.f.setItems(turnPage("turn-1", "", false)...) // a turn still open: one read, and the look is done
	reads := itemReadsCounted(ft.f)
	away(true)
	for range 5 {
		ft.s.watchTurn(context.Background(), task, []proto.Session{row})
		turnsQuiet(t, ft.s, task.ID)
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("%d transcript reads on a computer that is away, want none", got)
	}
	away(false)
	for range 3 {
		ft.s.watchTurn(context.Background(), task, []proto.Session{row})
		turnsQuiet(t, ft.s, task.ID)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("%d transcript reads once it is back, want 1 (the row did not move after it)", got)
	}
}
