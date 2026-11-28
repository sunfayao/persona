package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/taskleader"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// leaderCall is a tool call of the supervisor's (s1 on box) with args.
func leaderCall(s *Service, name string, args map[string]any) proto.ToolResult {
	raw, _ := json.Marshal(args)
	return s.mcpCall(context.Background(), "box", "s1", name, raw, false)
}

// A to-do's supervisor lists its own five tools beside the Agents' own, and no side channel: no task_spawn and no
// A2A, which it could not call either.
func TestASupervisorListsItsScopedToolsAndNoSideChannel(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	startTaskLeader(t, s, f)
	got := leaderToolNames(s.sessionMCPTools(ctx, "box", "s1", false, false))
	for _, want := range []string{"task_plan", "task_review", "agent_read", "agent_send", "task_scope", "report_to_supervisor"} {
		if !slices.Contains(got, want) {
			t.Errorf("the supervisor lacks %s: %v", want, got)
		}
	}
	for _, banned := range append([]string{"task_spawn", "machine_exec"}, assistantwire.A2AToolNames...) {
		if slices.Contains(got, banned) {
			t.Errorf("the supervisor lists %s: %v", banned, got)
		}
	}
	if got := leaderToolNames(s.runtimeLeaderTools(ctx, "box", "s1", false)); !slices.Equal(got,
		[]string{"task_plan", "task_review", "agent_read", "agent_send", "task_scope"}) {
		t.Fatalf("runtime leader tools: %v", got)
	}
	for _, name := range []string{"a2a_sessions", "task_spawn"} {
		if res := leaderCall(s, name, map[string]any{"title": "x", "prompt": "y"}); !res.IsError {
			t.Errorf("the supervisor called %s: %+v", name, res)
		}
	}
}

// The supervisor's agent_read, agent_send and task_scope reach its own workers only; agent_send carries no new work.
func TestTheSupervisorReachesOnlyItsOwnWorkers(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	root := startTaskLeader(t, s, f)
	w := planWorker(t, s, f, nil)
	other, err := s.createTaskTodo(ctx, "别的任务", "", "conv1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]map[string]any{
		"agent_read": {"todo_id": other.ID},
		"agent_send": {"todo_id": other.ID, "text": "hi"},
		"task_scope": {"todo_id": other.ID, "change": "x", "why": "y", "expect_minutes": 5},
	}
	for _, name := range []string{"agent_read", "agent_send", "task_scope"} {
		res := leaderCall(s, name, args[name])
		if !res.IsError || !strings.Contains(res.Content[0].Text, "not one of your workers") {
			t.Errorf("%s on another task: %+v", name, res)
		}
		res = s.mcpCall(ctx, "box", w.Agents[0].SessionID, name, json.RawMessage(`{"todo_id":"`+root.ID+`"}`), false)
		if !res.IsError || !strings.Contains(res.Content[0].Text, "unknown tool") {
			t.Errorf("a worker called the supervisor's %s: %+v", name, res)
		}
	}
	if res := leaderCall(s, "agent_send", map[string]any{"todo_id": w.ID, "text": "x", "task": map[string]any{"title": "new"}}); !res.IsError {
		t.Fatalf("agent_send took new work: %+v", res)
	}
	res := leaderCall(s, "agent_send", map[string]any{"todo_id": w.ID, "text": "测试先跑一遍再报告"})
	if res.IsError {
		t.Fatalf("a nudge to its worker: %+v", res)
	}
	waitFor(t, "the nudge in the worker's session", func() bool {
		return slices.ContainsFunc(sendsTo(f, "box", w.Agents[0].SessionID), func(m string) bool { return strings.Contains(m, "测试先跑一遍再报告") })
	})
	f.mu.Lock()
	f.items = []proto.Item{{Seq: 1, Role: "user", Kind: proto.KindText, Text: "改登录页"}, {Seq: 2, Role: "assistant", Kind: proto.KindText, Text: "布局改好了，测试全绿"}}
	f.mu.Unlock()
	if res := leaderCall(s, "agent_read", map[string]any{"todo_id": w.ID}); res.IsError || !strings.Contains(res.Content[0].Text, "测试全绿") {
		t.Fatalf("reading its worker: %+v", res)
	}
	if res := leaderCall(s, "task_scope", map[string]any{"todo_id": root.ID, "change": "加时间", "why": "worker 多", "done_when": []string{"x"}}); !res.IsError ||
		!strings.Contains(res.Content[0].Text, "main conversation's to change") {
		t.Fatalf("the supervisor changed its own to-do's criteria: %+v", res)
	}
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("the main conversation was told: %q", got)
	}
}

// A supervised to-do's own routine signals never reach the main conversation: its estimate goes to the supervisor,
// a pause wakes nobody, and the judge does not judge the supervisor's stops.
func TestASupervisedTodosRoutineSignalsNeverReachMain(t *testing.T) {
	s, f := newQuietService(t)
	root := startTaskLeader(t, s, f)
	if skip := autoDoneSkip(root, time.Now(), true); skip != "supervisor" {
		t.Fatalf("the judge would judge a supervisor's pause: %q", skip)
	}
	// A pause wakes nobody; a linked check's news goes to the supervisor, marked as its own to-do's.
	s.signal(superviseSignal{Todo: root.ID, Kind: sigStalled, Key: "stalled@1", Minutes: 40})
	s.signal(superviseSignal{Todo: root.ID, Kind: sigCheck, Report: checkNoProgressReport, Key: "check@1", Minutes: 50})
	m := gotMessage(t, f, "Your own to-do", "scheduled check")
	if strings.Contains(m, "no progress for 40 minutes") {
		t.Fatalf("the supervisor was told of its own pause:\n%s", m)
	}
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("the main conversation was told of the supervisor's own to-do: %q", got)
	}
	for _, kind := range []string{sigStalled, sigIdle, sigEstimateDue, sigOrphaned, sigCheck} {
		if taskleader.Route(kind, "", taskleader.SubjectRoot) == taskleader.ToMain {
			t.Fatalf("%s goes to main", kind)
		}
	}
}

// The supervisor's done is its to-do's completion only with every worker closed and every criterion evidenced; then
// the main conversation gets one review with the hub's worker record, and may accept it.
func TestASupervisorsCompletionIsGuardedAndReachesMainOnce(t *testing.T) {
	s, f := newQuietService(t)
	root := startTaskLeader(t, s, f)
	w := planWorker(t, s, f, nil)
	done := map[string]any{"kind": "done", "text": "全部做完", "criteria": []map[string]any{{"number": 1, "state": "met", "evidence": "PR 7 合并了"}}}
	if res := leaderCall(s, "report_to_supervisor", map[string]any{"kind": "done", "text": "全部做完"}); !res.IsError ||
		!strings.Contains(res.Content[0].Text, "still open") || !strings.Contains(res.Content[0].Text, "criteria not met") {
		t.Fatalf("done with an open worker and no evidence: %+v", res)
	}
	if res := leaderCall(s, "task_review", map[string]any{"todo_id": w.ID, "verdict": "accepted", "note": "ok", "checks": completeReviewChecks(todoNow(s, w.ID))}); res.IsError {
		t.Fatalf("accepting the worker: %+v", res)
	}
	if res := leaderCall(s, "report_to_supervisor", map[string]any{"kind": "done", "text": "全部做完"}); !res.IsError || !strings.Contains(res.Content[0].Text, "criteria not met") {
		t.Fatalf("done without evidence: %+v", res)
	}
	if got := f.notices(); len(got) != 0 {
		t.Fatalf("a refused completion reached main: %q", got)
	}
	if res := leaderCall(s, "report_to_supervisor", done); res.IsError || !strings.Contains(res.Content[0].Text, "Reported as complete") {
		t.Fatalf("the completion: %+v", res)
	}
	if v := todoNow(s, root.ID).Supervision; v.State != reviewPending {
		t.Fatalf("the to-do does not wait for review: %+v", v)
	}
	var m string
	waitFor(t, "one completion in the main conversation", func() bool {
		for _, n := range f.notices() {
			if strings.Contains(n, "its supervisor Agent checked every criterion") {
				m = n
				return true
			}
		}
		return false
	})
	if !strings.Contains(m, w.ID) || !strings.Contains(m, "accepted") || !strings.Contains(m, "PR 7 合并了") {
		t.Fatalf("the completion lacks the worker record or the evidence:\n%s", m)
	}
	if n := len(f.notices()); n != 1 {
		t.Fatalf("the main conversation got %d messages", n)
	}
}

// The main conversation accepts a supervised to-do only once its workers are closed, and cancelling it cancels them.
func TestASupervisedTodoClosesWithItsWorkers(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	root := startTaskLeader(t, s, f)
	w := planWorker(t, s, f, nil)
	raw, _ := json.Marshal(map[string]any{"todo_id": root.ID, "verdict": "accepted", "checks": completeReviewChecks(todoNow(s, root.ID))})
	if _, err := s.toolTaskReview(ctx, "conv1", raw); err == nil || !strings.Contains(err.Error(), "open worker") {
		t.Fatalf("accepted with an open worker: %v", err)
	}
	raw, _ = json.Marshal(map[string]any{"todo_id": root.ID, "verdict": "cancelled", "note": "不做了"})
	if _, err := s.toolTaskReview(ctx, "conv1", raw); err != nil {
		t.Fatal(err)
	}
	s.rehomeLeaderlessWorkers(ctx)
	if cur := todoNow(s, w.ID); cur.DoneAt == nil || cur.Supervision.State != reviewCancelled || !strings.Contains(cur.CancelledReason+cur.DoneReason, "cancelled") {
		t.Fatalf("the worker of a cancelled to-do: %+v", cur)
	}
	if cur := todoNow(s, w.ID); cur.Supervision.Supervisor == "conv1" {
		t.Fatal("the worker was handed to the main conversation instead of cancelled")
	}
}

// Work of a supervised to-do goes to its supervisor: the main conversation's task_plan on it or on a piece of it is
// refused, and an Agent on no task joins it as a worker through agent_send's task.todo_id.
func TestMainSendsASupervisedTodosWorkThroughItsSupervisor(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	root := startTaskLeader(t, s, f)
	task := map[string]any{"title": "补文档", "prompt": "补文档", "role": "worker", "machine": "box", "cwd": "/code/app", "readonly": false,
		"why": "x", "done_when": []string{"文档写好"}, "todo_id": root.ID}
	raw, _ := json.Marshal(map[string]any{"tasks": []any{task}})
	if _, err := s.toolTaskPlan(ctx, "conv1", raw); err == nil || !strings.Contains(err.Error(), "supervisor Agent") {
		t.Fatalf("main's task_plan on a supervised to-do: %v", err)
	}
	f.setSessions("box", proto.Session{SessionID: "s1", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusIdle},
		proto.Session{SessionID: "free0001", MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusIdle, UpdatedAt: time.Now()})
	res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "free0001", "text": "接着把测试补完",
		"task": map[string]any{"title": "补测试", "done_when": []string{"测试通过"}, "todo_id": root.ID}})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !strings.Contains(res, "is now worker") {
		t.Fatalf("attach answer: %s", res)
	}
	var w Todo
	for _, o := range todosOpenedBy(s, "box/s1") {
		for _, a := range o.Agents {
			if a.SessionID == "free0001" {
				w = o
			}
		}
	}
	if w.ID == "" || w.Supervision == nil || w.Supervision.Supervisor != "box/s1" || w.Agents[0].Source != sourceWorker || w.Agents[0].Via != root.ID {
		t.Fatalf("the attached worker: %+v %+v", w, w.Supervision)
	}
}

// Every tool the supervisor's brief, its messages and their rules name is one it lists: nothing sends it to a tool it
// lacks (as TestRoundToolsAgreeWithSupervisionInstructions keeps a round's).
func TestTheSupervisorsTextsNameOnlyItsOwnTools(t *testing.T) {
	listed := map[string]bool{}
	for _, tool := range append(leaderToolList(), (&Service{}).listedAgentTools()...) {
		listed[tool.Name] = true
	}
	known := append(slices.Clone(toolNames), "task_spawn", "machine_exec")
	known = append(known, assistantwire.A2AToolNames...)
	for _, tool := range (&Service{}).AgentTools() {
		known = append(known, tool.Name)
	}
	texts := []string{prompts.RenderTaskLeader(prompts.TaskLeader{}), prompts.RenderTaskLeader(prompts.TaskLeader{Terminal: true})}
	for _, kind := range taskleader.Kinds {
		for _, own := range []bool{false, true} {
			texts = append(texts, prompts.RenderWorkerSupervise([]prompts.SuperviseEntry{{ID: "todo_x", Title: "x", Kind: kind, Own: own}}))
		}
	}
	for _, text := range texts {
		for _, name := range known {
			if strings.Contains(name, "_") && strings.Contains(text, name) && !listed[name] {
				t.Errorf("the supervisor is told of %s, which it does not list:\n%.300s", name, text)
			}
		}
	}
}

// The hub marks the supervisor's message as supervision, as it marks a conversation's (its chat folds it); the
// earlier shape is read too, and a line that only starts alike is not.
func TestASupervisorsMessageIsMarkedAsSupervision(t *testing.T) {
	text := prompts.RenderWorkerSupervise([]prompts.SuperviseEntry{{ID: "todo_x", Title: "x", Kind: "review"}})
	if in := prompts.ParseInjected(text); in == nil || in.State != "supervise" || in.Level != "triage" {
		t.Fatalf("the supervisor's message: %+v", in)
	}
	legacy := "Persona · supervision: 2 of the worker tasks you handed out with task_plan need you. This message is not from the user.\n\n1. x"
	if in := prompts.ParseInjected(legacy); in == nil || in.State != "supervise" {
		t.Fatalf("the earlier shape: %+v", in)
	}
	if in := prompts.ParseInjected("Persona · supervision: what does it mean?"); in != nil {
		t.Fatalf("a look-alike line: %+v", in)
	}
}

// A supervisor main hands out with no computer or directory runs on the assistant's own computer, in a home
// directory, writable and in bypassPermissions (a plan mode would stop each of its tool calls on a card), on the
// light preset when the profile in use has it and no start-defaults rule decides a leader's start.
func TestASupervisorStartsHereOnItsOwnPreset(t *testing.T) {
	s, _ := newQuietServiceOn(t, &memStore{})
	ctx := context.Background()
	t1 := PlanTask{Role: roleLeader, Readonly: true, Permission: "plan"}
	s.leaderDefaults(ctx, &t1)
	if t1.Machine != s.settings(ctx).MachineID || t1.Machine == "" || t1.Cwd == "" || t1.Readonly || t1.Permission != "bypassPermissions" || t1.Tier != "" {
		t.Fatalf("defaults without a preset: %+v", t1)
	}
	saveTier(t, s, leaderTier, ModelTier{Launcher: "agy", Model: "gemini-example"})
	t2 := PlanTask{Role: roleLeader, Machine: "box", Cwd: "/code/app"}
	s.leaderDefaults(ctx, &t2)
	if t2.Machine != "box" || t2.Cwd != "/code/app" || t2.Tier != leaderTier {
		t.Fatalf("defaults with the light preset: %+v", t2)
	}
	t3 := PlanTask{Role: roleLeader, Tier: "architect"}
	s.leaderDefaults(ctx, &t3)
	if t3.Tier != "architect" {
		t.Fatalf("the caller's tier was replaced: %+v", t3)
	}
	s.startDefaultsFor = func(context.Context, StartFacts) (StartDefaults, bool, error) {
		return StartDefaults{Presets: []PresetChoice{{Name: "owner-pick"}}}, true, nil
	}
	t4 := PlanTask{Role: roleLeader}
	s.leaderDefaults(ctx, &t4)
	if t4.Tier != "" {
		t.Fatalf("a start-defaults rule for leaders was overridden: %+v", t4)
	}
}
