package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// autoRequest is newSupervisedService with the setting at its default (auto): a task of the main conversation conv1, its
// Agent s1 on box at work. The next Agent the fake hub starts is s2.
func autoRequest(t *testing.T) (*Service, *fakeHub, Todo) {
	t.Helper()
	s, f, todo := newSupervisedService(t)
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = "" })
	listAutoAgents(s, f)
	return s, f, todo
}

// listAutoAgents lists every Agent a to-do holds as a running session on its computer (the supervisor's is the
// assistant's own, with the main conversation beside it).
func listAutoAgents(s *Service, f *fakeHub) {
	byMachine := map[string][]proto.Session{"server": {{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}}}
	for _, t := range s.todosOf(context.Background(), "*") {
		for _, a := range t.Agents {
			byMachine[a.MachineID] = append(byMachine[a.MachineID], proto.Session{SessionID: a.SessionID, MachineID: a.MachineID, Cwd: a.Cwd,
				Agent: "claude", Status: proto.StatusWorking, UpdatedAt: time.Now()})
		}
	}
	for machine, list := range byMachine {
		f.setSessions(machine, list...)
	}
}

// reviewWaits puts the to-do in front of its supervisor's review and raises the signal for it.
func reviewWaits(t *testing.T, s *Service, id string) {
	t.Helper()
	if _, _, err := s.updateSupervision(context.Background(), id, func(v *Supervision) bool { v.State = reviewPending; return true }); err != nil {
		t.Fatal(err)
	}
	s.signal(superviseSignal{Todo: id, Kind: sigReview, Key: "review@" + id})
}

// supervisorAgentOf is the supervisor Agent row of to-do id, once it has one.
func supervisorAgentOf(s *Service, id string) (TodoAgent, bool) {
	t, _ := s.todoNow(id)
	return t.supervisorAgent()
}

// toSession are the texts sent into session on box.
func toSession(f *fakeHub, session string) string {
	return strings.Join(sendsTo(f, "server", session), "\n---\n")
}

func TestTheFirstReviewStartsOneSupervisorAndTellsTheMainConversationNothing(t *testing.T) {
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { _, ok := supervisorAgentOf(s, todo.ID); return ok })
	lead, _ := supervisorAgentOf(s, todo.ID)
	if lead.SessionID != "s2" || lead.MachineID != "server" || lead.Source != sourceLeader || f.agentCount() != 2 {
		t.Fatalf("supervisor row %+v after %d starts", lead, f.agentCount())
	}
	listAutoAgents(s, f)
	waitFor(t, "the review in the supervisor's session", func() bool { return strings.Contains(toSession(f, "s2"), "waits for your review") })
	cur, _ := s.todoNow(todo.ID)
	v := cur.Supervision
	if !cur.memberSupervised() || v.Supervisor != leaderRef("server", "s2") || v.SupervisorSource != autosupervise.Auto || v.Above != "conv1" {
		t.Fatalf("not adopted: %+v", v)
	}
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("the main conversation was told: %q", got)
	}
	if got := toSession(f, "s1"); got != "" {
		t.Fatalf("the Agent already working was told something: %q", got)
	}
	first := toSession(f, "s2")
	for _, want := range []string{"Persona · supervision", "修登录崩溃", "1. PR 合并到 master", "2. 浅色 / 深色截图贴进 PR"} {
		if !strings.Contains(first, want) {
			t.Errorf("the supervisor's messages lack %q:\n%s", want, first)
		}
	}
	f.mu.Lock()
	start := f.starts[len(f.starts)-1]
	f.mu.Unlock()
	if start["cwd"] == nil {
		t.Fatalf("start: %v", start)
	}
	if strings.Contains(start["prompt"].(string), "hand its parts to workers") || start["permission_mode"] != "bypassPermissions" {
		t.Fatalf("the supervisor's start: %v", start)
	}
}

func TestExplicitModeSendsEverythingToTheMainConversation(t *testing.T) {
	s, f, todo := autoRequest(t)
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = autosupervise.Explicit })
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the main conversation's message", func() bool { return len(f.notices()) == 1 })
	if _, ok := supervisorAgentOf(s, todo.ID); ok || f.agentCount() != 1 {
		t.Fatalf("a supervisor was started in explicit mode (%d starts)", f.agentCount())
	}
}

func TestAFailedSupervisorStartFallsBackToMainAndIsNotTriedForThirtyMinutes(t *testing.T) {
	s, f, todo := autoRequest(t)
	clock := time.Now()
	s.supervise.autoNow = func() time.Time { return clock }
	f.mu.Lock()
	f.failStart = errors.New("no launcher")
	f.mu.Unlock()
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the review in the main conversation", func() bool { return len(f.notices()) == 1 })
	starts := len(f.starts)
	if _, ok := supervisorAgentOf(s, todo.ID); ok {
		t.Fatal("a supervisor row after a failed start")
	}
	if s.superviseAuto(context.Background(), s.settings(context.Background()), superviseSignal{Todo: todo.ID, Kind: sigEstimateDue, Key: "estimate@1"}) {
		t.Fatal("a signal was held for a supervisor within the cooldown")
	}
	f.mu.Lock()
	again := len(f.starts)
	f.failStart = nil
	f.mu.Unlock()
	if again != starts {
		t.Fatalf("a new start within the cooldown: %d starts, %d before", again, starts)
	}
	clock = clock.Add(autosupervise.Cooldown + time.Second)
	s.signal(superviseSignal{Todo: todo.ID, Kind: sigOverdue, Key: "overdue@1", Minutes: 90})
	waitFor(t, "the supervisor Agent after the cooldown", func() bool { _, ok := supervisorAgentOf(s, todo.ID); return ok })
}

func TestTwoSignalsOfOneRequestStartOneSupervisor(t *testing.T) {
	s, f, todo := autoRequest(t)
	ctx := context.Background()
	here := s.settings(ctx)
	g1 := superviseSignal{Todo: todo.ID, Kind: sigReview, Key: "r1"}
	g2 := superviseSignal{Todo: todo.ID, Kind: sigEstimateDue, Key: "e1"}
	if !s.superviseAuto(ctx, here, g1) || !s.superviseAuto(ctx, here, g2) {
		t.Fatal("a signal was not held for the supervisor")
	}
	waitFor(t, "the supervisor Agent", func() bool { _, ok := supervisorAgentOf(s, todo.ID); return ok })
	waitFor(t, "the gate to be free", func() bool {
		return !s.autoGate().Cooling(todo.ID) && s.autoGate().Begin(todo.ID) != autosupervise.Wait
	})
	if f.agentCount() != 2 {
		t.Fatalf("%d Agents started, want the worker and one supervisor", f.agentCount())
	}
}

// A to-do whose supervisor Agent judges what its Agents raise sends the owner's decisions to the conversation above it
// and never to the Agent.
func TestADecisionKindOfAMemberSupervisedTodoGoesToTheConversationAbove(t *testing.T) {
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	waitFor(t, "the review in the supervisor's session", func() bool { return strings.Contains(toSession(f, "s2"), "waits for your review") })
	before := len(sendsTo(f, "server", "s2"))
	s.signal(superviseSignal{Todo: todo.ID, Kind: sigStarted, Key: "started@1"})
	waitFor(t, "the main conversation's message", func() bool { return len(f.notices()) == 1 })
	if got := len(sendsTo(f, "server", "s2")); got != before {
		t.Fatalf("the supervisor was told a kind that goes up: %q", sendsTo(f, "server", "s2")[before:])
	}
}

// The supervisor's agent_read of its to-do reads the Agent that works on it, never its own session.
func TestTheSupervisorReadsTheWorkAgentNeverItself(t *testing.T) {
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	res := s.mcpCall(context.Background(), "server", "s2", "agent_read", json.RawMessage(`{"todo_id":"`+todo.ID+`"}`), false)
	if res.IsError && !strings.Contains(res.Content[0].Text, "") {
		t.Fatal(res)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	read := map[string]bool{}
	for _, c := range f.calls {
		if c.Method == DeviceSessionItems {
			raw, _ := json.Marshal(c.Params)
			var p struct {
				SessionID string `json:"session_id"`
			}
			_ = json.Unmarshal(raw, &p)
			read[p.SessionID] = true
		}
	}
	if read["s2"] || !read["s1"] {
		t.Fatalf("sessions read: %v (result %+v)", read, res)
	}
}

// The supervisor's report that its member-supervised to-do is done is refused with the instruction to review it; its
// questions go up.
func TestTheSupervisorsDoneOnAMemberSupervisedTodoIsRefused(t *testing.T) {
	s, f, todo := autoRequest(t)
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	res := s.agentTool(context.Background(), "server", "s2", "report_to_supervisor", json.RawMessage(`{"kind":"done","text":"all merged"}`))
	if !res.IsError || !strings.Contains(res.Content[0].Text, "task_review accepted") {
		t.Fatalf("done: %+v", res)
	}
	res = s.agentTool(context.Background(), "server", "s2", "report_to_supervisor", json.RawMessage(`{"kind":"question","text":"which branch?"}`))
	if res.IsError {
		t.Fatalf("question: %+v", res)
	}
	waitFor(t, "the question in the main conversation", func() bool {
		return slices.ContainsFunc(f.notices(), func(n string) bool { return strings.Contains(n, "which branch?") })
	})
}

// The conversation above keeps task_scope, task_review and a plain agent_send on a to-do it handed out.
func TestTheConversationAboveKeepsItsToolsOnTheToDoItHandedOut(t *testing.T) {
	s, f, todo := autoRequest(t)
	ctx := context.Background()
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	if _, err := s.toolTaskScope(ctx, "conv1", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"more time","why":"slow tests","expect_minutes":90}`)); err != nil {
		t.Fatalf("task_scope: %v", err)
	}
	cur, _ := s.todoNow(todo.ID)
	other := Todo{ID: "t", Origin: "side", Supervision: &Supervision{Supervisor: leaderRef("server", "s2"), Above: "main2"}}
	if err := othersTask(proto.Session{}, other, "main2"); err != nil {
		t.Fatalf("the conversation above is refused the Agent: %v", err)
	}
	if err := othersTask(proto.Session{}, other, "third"); err == nil {
		t.Fatal("a stranger is let in")
	}
	if _, err := s.sendToTask(ctx, cur, "please also update the docs"); err != nil {
		t.Fatalf("a plain send to the to-do: %v", err)
	}
	if got := sendsTo(f, "box", "s1"); len(got) != 1 || len(sendsTo(f, "server", "s2")) > 1 {
		t.Fatalf("the send went to the wrong Agent: s1 %q", got)
	}
	if _, err := s.toolTaskReview(ctx, "conv1", json.RawMessage(`{"todo_id":"`+cur.ID+`","verdict":"cancelled","note":"not needed"}`)); err != nil {
		t.Fatalf("task_review: %v", err)
	}
}

// A request's pieces share one supervisor, and a piece the main conversation plans later is its worker.
func TestPiecesOfOneRequestShareOneSupervisorAndLaterPiecesJoinIt(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	root, err := s.createTodo(ctx, "login rework", "all of it", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	plan := func(title string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"title": title, "prompt": title, "machine": "box", "cwd": "/code/app",
			"role": "worker", "part_of": root.ID, "readonly": true, "permission": "plan", "why": "box has the repo", "done_when": []string{title + " done"}}}})
		if _, err := s.toolTaskPlan(ctx, "conv1", raw); err != nil {
			t.Fatal(err)
		}
	}
	plan("page")
	plan("tests")
	waitFor(t, "both pieces to run", func() bool { return f.agentCount() == 2 })
	waitFor(t, "both links", func() bool {
		n := 0
		for _, c := range s.todosOf(ctx, "*") {
			if c.PartOf == root.ID && len(c.Agents) == 1 {
				n++
			}
		}
		return n == 2
	})
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = "" })
	listAutoAgents(s, f)
	var piece Todo
	for _, c := range s.todosOf(ctx, "*") {
		if c.PartOf == root.ID && c.Title == "page" {
			piece = c
		}
	}
	reviewWaits(t, s, piece.ID)
	waitFor(t, "the request's supervisor", func() bool { _, ok := supervisorAgentOf(s, root.ID); return ok })
	if _, ok := supervisorAgentOf(s, piece.ID); ok {
		t.Fatal("a piece got a supervisor of its own")
	}
	lead, _ := supervisorAgentOf(s, root.ID)
	waitFor(t, "both pieces adopted", func() bool {
		n := 0
		for _, c := range s.todosOf(ctx, "*") {
			if c.PartOf == root.ID && c.Supervision != nil && c.Supervision.Supervisor == leaderRef(lead.MachineID, lead.SessionID) && c.Supervision.Above == "conv1" {
				n++
			}
		}
		return n == 2
	})
	// A piece planned afterwards names the supervisor from its creation.
	listAutoAgents(s, f)
	plan("docs")
	waitFor(t, "the late piece", func() bool {
		for _, c := range s.todosOf(ctx, "*") {
			if c.Title == "docs" && c.Supervision != nil && c.Supervision.Supervisor == leaderRef(lead.MachineID, lead.SessionID) {
				return true
			}
		}
		return false
	})
	waitFor(t, "the late piece's Agent", func() bool { return f.agentCount() == 4 })
	if f.agentCount() != 4 {
		t.Fatalf("%d Agents started: three pieces and one supervisor", f.agentCount())
	}
	// todo_id of the supervised root itself stays refused.
	raw, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"title": "again", "prompt": "again", "machine": "box", "cwd": "/code/app",
		"role": "worker", "todo_id": root.ID, "readonly": true, "permission": "plan", "why": "x", "done_when": []string{"x"}}}})
	if _, err := s.toolTaskPlan(ctx, "conv1", raw); err == nil || !strings.Contains(err.Error(), "supervisor Agent") {
		t.Fatalf("todo_id of a supervised root: %v", err)
	}
}

func TestGoldenTaskSupervisorAttached(t *testing.T) {
	golden(t, "task_supervisor_attached", prompts.RenderTaskSupervisorAttached(prompts.TaskSupervisorAttached{Todos: []prompts.SupervisedTodo{
		{ID: "todo_example1", Title: "Example task", Machine: "box", Criteria: []string{"The change is merged", "The checks pass"}},
		{ID: "todo_example2", Title: "Another task"},
	}}))
}

// The supervisor closes the to-do its Agents work on with its own task_review, by the path it closes a worker; the
// scope of that to-do is its to change, and it is refused while a piece of it is open.
func TestTheSupervisorClosesAMemberSupervisedTodoWithItsOwnReview(t *testing.T) {
	s, f, todo := autoRequest(t)
	ctx := context.Background()
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	listAutoAgents(s, f)
	scope := s.mcpCall(ctx, "server", "s2", "task_scope", json.RawMessage(`{"todo_id":"`+todo.ID+`","change":"drop the screenshots","why":"owner said so","drop":[2]}`), false)
	if scope.IsError {
		t.Fatalf("task_scope on the to-do its Agents work on: %+v", scope)
	}
	review := s.mcpCall(ctx, "server", "s2", "task_review", json.RawMessage(`{"todo_id":"`+todo.ID+`","verdict":"accepted","checks":[{"number":1,"met":true,"evidence":"PR 9 merged"}]}`), false)
	if review.IsError {
		t.Fatalf("task_review accepted: %+v", review)
	}
	if cur, _ := s.todoNow(todo.ID); cur.DoneAt == nil || cur.Supervision.state(cur) != reviewAccepted {
		t.Fatalf("not closed: %+v", cur)
	}
}

// A side conversation that handed a to-do out keeps task_scope and task_review on it after a supervisor Agent took it
// over (its Above), and a stranger does not.
func TestASideConversationAboveASupervisorKeepsScopeAndReview(t *testing.T) {
	ctx := context.Background()
	s, f := newQuietService(t)
	side := capableRuntimeTestSide()
	f.setSessions("server", side)
	f.mu.Lock()
	f.known[side.SessionID] = true
	f.mu.Unlock()
	setSettings(t, s, func(v *Settings) { v.Main = "" })
	if _, err := s.toolTaskPlan(ctx, side.SessionID, json.RawMessage(runtimePlan)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the worker to link", func() bool { return len(onlyTodo(s).Agents) == 1 })
	task := onlyTodo(s)
	if err := s.linkTodoAgentWithSource(ctx, task.ID, "server", "sup1", "supervisor", "/", "claude", "", sourceLeader, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.updateSupervision(ctx, task.ID, func(v *Supervision) bool {
		v.Supervisor, v.SupervisorSource, v.Above = leaderRef("server", "sup1"), autosupervise.Auto, side.SessionID
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := s.todoNow(task.ID); !cur.memberSupervised() {
		t.Fatalf("not member-supervised: %+v", cur.Supervision)
	}
	scope := map[string]any{"todo_id": task.ID, "change": "more time", "why": "slow tests", "expect_minutes": 60}
	if _, err := runJSON(t, s, "stranger", "task_scope", scope); err == nil || !strings.Contains(err.Error(), "supervised by another conversation") {
		t.Fatalf("a stranger's task_scope: %v", err)
	}
	if _, err := runJSON(t, s, side.SessionID, "task_scope", scope); err != nil {
		t.Fatalf("task_scope of the conversation above: %v", err)
	}
	if _, err := runJSON(t, s, "stranger", "task_review", map[string]any{"todo_id": task.ID, "verdict": "cancelled", "note": "no"}); err == nil {
		t.Fatal("a stranger's task_review went through")
	}
	if _, err := runJSON(t, s, side.SessionID, "task_review", map[string]any{"todo_id": task.ID, "verdict": "accepted", "note": "verified"}); err != nil {
		t.Fatalf("task_review of the conversation above: %v", err)
	}
}

// assistant.todo_supervisor is in the settings registry: unset reads auto, explicit and auto are the only values.
func TestTodoSupervisorSettingIsInTheRegistry(t *testing.T) {
	ctx := context.Background()
	s, _ := newQuietService(t)
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = "" })
	for _, f := range s.profileFields() {
		if f.key != "assistant.todo_supervisor" {
			continue
		}
		if got, err := f.get(ctx); err != nil || got != autosupervise.Auto {
			t.Fatalf("unset reads %v, %v", got, err)
		}
		for _, bad := range []string{`"off"`, `true`} {
			if _, err := f.decodeValue(json.RawMessage(bad)); err == nil {
				t.Errorf("%s was taken", bad)
			}
		}
		val, err := f.decodeValue(json.RawMessage(`"explicit"`))
		if err != nil || f.write(ctx, val) != nil {
			t.Fatalf("explicit: %v", err)
		}
		if got, _ := f.get(ctx); got != autosupervise.Explicit || s.settings(ctx).TodoSupervisor != autosupervise.Explicit {
			t.Fatalf("after writing explicit: %v", got)
		}
		return
	}
	t.Fatal("assistant.todo_supervisor is not in the registry")
}

// A to-do planned with role leader has its own supervisor Agent: its completion and its questions reach the main
// conversation as before, with no second supervisor started for it.
func TestAPlannedLeadersCompletionStillReachesMainWithNoSecondSupervisor(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	root := startTaskLeader(t, s, f)
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = "" })
	res := s.agentTool(ctx, "box", "s1", "report_to_supervisor", json.RawMessage(`{"kind":"done","text":"all merged","criteria":[{"number":1,"state":"met","evidence":"PR 1 merged"}]}`))
	if res.IsError {
		t.Fatalf("the leader's done: %+v", res)
	}
	waitFor(t, "the completion in the main conversation", func() bool { return len(f.notices()) == 1 })
	if f.agentCount() != 1 {
		t.Fatalf("%d Agents started: a second supervisor for %s", f.agentCount(), root.ID)
	}
}

// A piece that joins a request whose supervisor Agent cannot be reached goes to the main conversation as before, and the
// request is not tried again at once.
func TestAnUnreachableSupervisorSendsALaterPieceToMainAndCoolsTheRequest(t *testing.T) {
	s, f, todo := autoRequest(t)
	ctx := context.Background()
	reviewWaits(t, s, todo.ID)
	waitFor(t, "the supervisor Agent", func() bool { cur, _ := s.todoNow(todo.ID); return cur.memberSupervised() })
	piece, err := s.createTodo(ctx, "late piece", "more", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.editTodo(ctx, piece.ID, db.TodoEdit{PartOf: &todo.ID}); err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, piece.ID, "conv1", []string{"merged"}, 0)
	listAutoAgents(s, f)
	f.setSessions("server", proto.Session{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle}) // the supervisor's session is gone
	reviewWaits(t, s, piece.ID)
	waitFor(t, "the piece in the main conversation", func() bool {
		return slices.ContainsFunc(f.notices(), func(n string) bool { return strings.Contains(n, "late piece") })
	})
	if !s.autoGate().Cooling(todo.ID) || f.agentCount() != 2 {
		t.Fatalf("cooling %v, %d starts", s.autoGate().Cooling(todo.ID), f.agentCount())
	}
}
