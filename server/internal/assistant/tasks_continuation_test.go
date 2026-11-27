package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestContinuationKeepsAnAgentNowWorkingOnAnotherTodo(t *testing.T) {
	e := newThreadEnv(t, nil)
	agent := TodoAgent{MachineID: "box", SessionID: "agent01"}
	putTodo(e.s, Todo{ID: "todo_old", Title: "Original task", Origin: "m1", Agents: []TodoAgent{agent}})
	putTodo(e.s, Todo{ID: "todo_current", Title: "Current task", Origin: "m1", Agents: []TodoAgent{agent}})

	if _, err := e.s.toolTaskPlan(context.Background(), "m1", continuesArgs("todo_old")); err != nil {
		t.Fatal(err)
	}
	old := newAgentOn(t, e, "todo_old")
	if old.ReleasedAt == nil || old.DismissedAt != nil {
		t.Fatalf("the stale original link was not released into history: %+v", old)
	}
	if len(e.f.stopped) != 0 {
		t.Fatalf("continuing the original task stopped another task's Agent: %v", e.f.stopped)
	}
	current := todoNow(e.s, "todo_current")
	if len(current.Agents) != 1 || !current.Agents[0].active() {
		t.Fatalf("the other task lost its live Agent: %+v", current)
	}
	e.s.todos.mu.RLock()
	owner := e.s.todos.activeBySession[[2]string{agent.MachineID, agent.SessionID}]
	e.s.todos.mu.RUnlock()
	if owner != current.ID {
		t.Fatalf("the Agent's active owner changed to %q, want %q", owner, current.ID)
	}
	if todoCount(e.s) != 2 {
		t.Fatalf("continuation created an extra to-do: %d", todoCount(e.s))
	}
}

func TestContinuationStopFailureDoesNotSplitOrReleaseTodo(t *testing.T) {
	e := newThreadEnv(t, nil)
	putTodo(e.s, Todo{ID: "todo_x", Title: "Ship the fix", Origin: "m1",
		Agents: []TodoAgent{{MachineID: "box", SessionID: "agent01"}}})
	e.f.retireErr = errors.New("stop refused")
	result, err := e.s.toolTaskPlan(context.Background(), "m1", continuesArgs("todo_x"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "stop refused") {
		t.Fatalf("stop failure was not reported: %q", result)
	}
	if todoCount(e.s) != 1 || len(e.f.starts) != 0 || todoNow(e.s, "todo_x").Agents[0].ReleasedAt != nil {
		t.Fatalf("failed takeover changed ownership or started a replacement: %q", result)
	}
	e.s.tasks.mu.Lock()
	defer e.s.tasks.mu.Unlock()
	if e.s.tasks.reusing["todo_x"] {
		t.Fatal("failed takeover retained its admission hold")
	}
}

func TestContinuationCannotTakeAnotherConversationsTask(t *testing.T) {
	e := newThreadEnv(t, nil)
	putTodo(e.s, Todo{ID: "todo_x", Title: "Other task", Origin: "c1x1x1",
		Agents: []TodoAgent{{MachineID: "box", SessionID: "agent01"}}})
	_, err := e.s.toolTaskPlan(context.Background(), "m1", continuesArgs("todo_x"))
	if err == nil || !strings.Contains(err.Error(), "not opened from this conversation") {
		t.Fatalf("unauthorized continuation was not refused: %v", err)
	}
	if todoCount(e.s) != 1 || len(e.f.starts) != 0 || len(e.f.stopped) != 0 {
		t.Fatal("unauthorized continuation touched the original task")
	}
}

func TestContinuationUnknownComputerDoesNotPermitReplacement(t *testing.T) {
	e := newThreadEnv(t, nil)
	putTodo(e.s, Todo{ID: "todo_x", Title: "Ship the fix", Origin: "m1",
		Agents: []TodoAgent{{MachineID: "unknown", SessionID: "agent01"}}})
	res, err := e.s.toolTaskPlan(context.Background(), "m1", continuesArgs("todo_x"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "cannot confirm") || todoCount(e.s) != 1 || len(e.f.starts) != 0 {
		t.Fatalf("an unconfirmed predecessor permitted a replacement: %q", res)
	}
	if todoNow(e.s, "todo_x").Agents[0].ReleasedAt != nil {
		t.Fatal("unknown predecessor was released without confirmation")
	}
}

func TestReadonlyCodexContinuationBecomesWritableOnSameTodo(t *testing.T) {
	for _, closed := range []bool{false, true} {
		name := "open"
		if closed {
			name = "closed"
		}
		t.Run(name, func(t *testing.T) {
			e := newThreadEnv(t, nil)
			ctx := context.Background()
			e.f.launcherList = []proto.Launcher{{Name: "codex", Agent: "codex"}}
			e.f.ownModels = true
			first := readonlyTestTask("bypassPermissions")
			first.Launcher, first.Goal, first.ExpectMinutes = "codex", "Find and fix the failure", 10
			if _, err := e.s.toolTaskPlan(ctx, "m1", readonlyTaskArgs(t, first)); err != nil {
				t.Fatal(err)
			}
			original := planOf(t, e.s, "m1")
			id := original.Tasks[0].TodoID
			waitFor(t, "original read-only agent", func() bool { return len(todoNow(e.s, id).Agents) == 1 })
			old := todoNow(e.s, id).Agents[0]
			e.sessions("box", proto.Session{SessionID: old.SessionID, Agent: "codex", Status: proto.StatusWorking})
			if closed {
				e.must("PATCH", "/api/v1/todos/"+id, `{"done":true}`)
			}
			brief, _ := e.s.briefOfTodo(id)
			next := first
			next.Readonly, next.Permission = false, "bypassPermissions"
			next.Continues, next.ContinuesWhy = id, "Diagnosis is complete; implement the requested fix."
			next.Prompt = "Implement the fix and verify it by writing a temporary marker."
			res, err := e.s.toolTaskPlan(ctx, "m1", readonlyTaskArgs(t, next))
			if err != nil {
				t.Fatal(err)
			}
			// Writable work still waits for authorization; proposing a handoff must not stop the old Agent.
			if len(e.f.starts) != 1 || len(e.f.stopped) != 0 {
				t.Fatalf("planning changed running Agents: %s", res)
			}
			if (todoNow(e.s, id).DoneAt != nil) != closed {
				t.Fatal("planning reopened the to-do before Start")
			}
			plan, err := e.s.startPlan(ctx, planIDRe.FindString(res), nil, false)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "writable replacement on original to-do", func() bool {
				return len(todoNow(e.s, id).Agents) == 2
			})
			e.f.mu.Lock()
			starts := append([]map[string]any(nil), e.f.starts...)
			e.f.mu.Unlock()
			if len(starts) != 2 || starts[0]["permission_mode"] != "plan" || starts[1]["permission_mode"] != "bypassPermissions" {
				t.Fatalf("Codex permission transition: %+v", starts)
			}
			prompt, _ := starts[1]["prompt"].(string)
			if strings.Contains(prompt, "本任务只读") || !strings.Contains(prompt, "This continuation is writable") {
				t.Fatalf("writable replacement kept an ambiguous read-only prompt: %q", prompt)
			}
			if plan.Tasks[0].Readonly || plan.Tasks[0].TodoID != id || todoCount(e.s) != 1 {
				t.Fatalf("writable replacement split the task: %+v", plan.Tasks[0])
			}
			if kept, _ := e.s.briefOfTodo(id); kept.ID != brief.ID {
				t.Fatalf("permission transition replaced the brief: %+v", kept)
			}
			previous, err := e.s.loadPlan(ctx, original.ID)
			if err != nil || previous.Tasks[0].TodoID != "" {
				t.Fatalf("obsolete read-only binding remains: %+v (%v)", previous, err)
			}
			current := todoNow(e.s, id)
			if current.PlanID != original.ID {
				t.Fatalf("continuation rewrote the historical plan: %q, want %q", current.PlanID, original.ID)
			}
			replacement := current.Agents[1]
			if replacement.Source != "manual" {
				t.Fatalf("replacement is not the original assignment's Agent: %+v", replacement)
			}
			e.sessions("box", proto.Session{SessionID: replacement.SessionID, Agent: "codex", Launcher: "codex",
				Cwd: plan.Tasks[0].Cwd, Status: proto.StatusWorking})
			spawn := e.s.mcpCall(ctx, "box", replacement.SessionID, "task_spawn", json.RawMessage(spawnArgs), false)
			if spawn.IsError {
				t.Fatalf("the writable replacement could not spawn: %+v", spawn.Content)
			}
			e.f.mu.Lock()
			starts = append([]map[string]any(nil), e.f.starts...)
			e.f.mu.Unlock()
			if len(starts) != 3 || starts[2]["permission_mode"] != "bypassPermissions" {
				t.Fatalf("continuation child did not inherit the current assignment: %+v", starts)
			}
			childPrompt, _ := starts[2]["prompt"].(string)
			if strings.Contains(childPrompt, "本任务只读") {
				t.Fatalf("writable continuation's child was constrained to read-only: %q", childPrompt)
			}
			from := "m1"
			for _, to := range []string{"m2", "m3"} {
				if moved := e.s.handOverMainTasks(ctx, from, to); moved != 1 {
					t.Fatalf("main handover %s -> %s moved %d tasks", from, to, moved)
				}
				from = to
			}
			if handed := todoNow(e.s, id); handed.Origin != "m3" || handed.Supervision.Supervisor != "m3" {
				t.Fatalf("the to-do did not follow the main handovers: %+v", handed)
			}
			spawn = e.s.mcpCall(ctx, "box", replacement.SessionID, "task_spawn", json.RawMessage(spawnArgs), false)
			if spawn.IsError {
				t.Fatalf("the replacement could not spawn after main handovers: %+v", spawn.Content)
			}
			e.f.mu.Lock()
			starts = append([]map[string]any(nil), e.f.starts...)
			e.f.mu.Unlock()
			if len(starts) != 4 || starts[3]["permission_mode"] != "bypassPermissions" {
				t.Fatalf("handovers lost the current continuation assignment: %+v", starts)
			}
		})
	}
}

func TestContinuationTakeoverHoldPreventsConcurrentReplacement(t *testing.T) {
	e := newThreadEnv(t, nil)
	ctx := context.Background()
	putTodo(e.s, Todo{ID: "todo_x", Title: "Ship the fix", Origin: "m1",
		Agents: []TodoAgent{{MachineID: "box", SessionID: "agent01"}}})
	task := PlanTask{ReuseTodo: "todo_x", Continues: "todo_x"}
	if _, err := e.s.takeTodoForTask(ctx, "first-plan", "m1", task); err != nil {
		t.Fatal(err)
	}
	defer e.s.releaseTakenTodo("todo_x")
	if _, err := e.s.takeTodoForTask(ctx, "second-plan", "m1", task); err == nil || !strings.Contains(err.Error(), "another plan") {
		t.Fatalf("concurrent continuation was not refused: %v", err)
	}
	if len(e.f.stopped) != 1 || todoCount(e.s) != 1 {
		t.Fatalf("concurrent continuation stopped another Agent or created a to-do: stops=%v", e.f.stopped)
	}
}

func TestLegacyContinuationCardRebindsAtStart(t *testing.T) {
	e := newThreadEnv(t, nil)
	ctx := context.Background()
	putTodo(e.s, Todo{ID: "todo_x", Title: "Ship the fix", Origin: "m1",
		Agents: []TodoAgent{{MachineID: "box", SessionID: "agent01"}}})
	plan := &TaskPlan{ID: "legacy-plan", Conversation: "m1", State: planOpen,
		Tasks: []PlanTask{{Title: "Continue the fix", Prompt: "Verify the fix", Machine: "box", Cwd: "/code/app",
			Readonly: true, Continues: "todo_x", ContinuesWhy: "Follow-up verification"}}}
	if err := e.s.savePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	started, err := e.s.startPlan(ctx, plan.ID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	newAgentOn(t, e, "todo_x")
	if started.Tasks[0].TodoID != "todo_x" || todoCount(e.s) != 1 {
		t.Fatalf("legacy card created a second to-do: %+v", started.Tasks[0])
	}
}

func TestGoldenTaskContinuation(t *testing.T) {
	for _, readonly := range []bool{true, false} {
		name := "task_continuation.writable"
		if readonly {
			name = "task_continuation.readonly"
		}
		golden(t, name, prompts.RenderTaskContinuation(prompts.TaskContinuation{TodoID: "todo_example", Readonly: readonly}))
	}
}
