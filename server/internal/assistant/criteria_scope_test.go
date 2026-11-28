package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestCriterionOver200IsRefusedWhereItIsWritten(t *testing.T) {
	long := strings.Repeat("字", 201)
	s, f := newQuietService(t)
	f.setSessions("box", proto.Session{SessionID: "sec00001", MachineID: "box", Cwd: "/code/desk", Agent: "claude", Title: "秘书",
		Status: proto.StatusIdle, State: proto.StateIdle, UpdatedAt: time.Now()}, proto.Session{SessionID: "s1abcdef", MachineID: "box", Cwd: "/code/app", Agent: "claude", Title: "x",
		Status: proto.StatusWorking, State: proto.StateWorking, UpdatedAt: time.Now()})
	for name, call := range map[string]func() (string, error){
		"task_plan": func() (string, error) {
			return runJSON(t, s, "conv1", "task_plan", briefTask(map[string]any{"done_when": []string{"fine", long}}))
		},
		"agent_send": func() (string, error) {
			return runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "sec00001", "text": "go",
				"task": map[string]any{"title": "t", "done_when": []string{long}}})
		},
		"agent_supervise": func() (string, error) {
			return runJSON(t, s, "conv1", "agent_supervise", map[string]any{"machine": "box", "session_id": "s1abcdef", "done_when": []string{long}})
		},
	} {
		_, err := call()
		if err == nil || !strings.Contains(err.Error(), "201 characters") || !strings.Contains(err.Error(), "at most 200") {
			t.Fatalf("%s: %v", name, err)
		}
		if n := len(s.todos.todos); n != 0 {
			t.Fatalf("%s wrote %d to-dos", name, n)
		}
	}
}

func TestTaskScopeRefusesLongCriterionAndKeepsStoredOnes(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	long := strings.Repeat("x", 201)
	for _, args := range []map[string]any{{"done_when": []string{long}}, {"add": []string{long}}, {"edit": []map[string]any{{"number": 1, "text": long}}}} {
		args["todo_id"], args["change"], args["why"] = todo.ID, "reword", "clearer"
		if res, err := runJSON(t, s, "conv1", "task_scope", args); err == nil || !strings.Contains(err.Error(), "201 characters") {
			t.Fatalf("%v: %s %v", args, res, err)
		}
	}
	if got := todoNow(s, todo.ID).Supervision.DoneWhen; len(got) != 2 {
		t.Fatalf("criteria changed: %v", got)
	}
	// A criterion an older build clipped keeps working: it is stored as it is and can be named by number.
	legacy := strings.Repeat("y", 200) + "…"
	if _, _, err := s.updateSupervision(t.Context(), todo.ID, func(v *Supervision) bool { v.DoneWhen[0] = legacy; return true }); err != nil {
		t.Fatal(err)
	}
	checks := []map[string]any{{"number": 1, "met": true, "evidence": "ok"}, {"number": 2, "met": true, "evidence": "ok"}}
	if res, err := runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "accepted", "outcome": "done", "checks": checks}); err != nil ||
		!strings.Contains(res, "accepted and closed") {
		t.Fatalf("legacy criterion: %s %v", res, err)
	}
}

func TestTaskReviewChecksByNumber(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	review := func(checks ...map[string]any) (string, error) {
		return runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "accepted", "outcome": "done", "checks": checks})
	}
	met := func(extra map[string]any) map[string]any {
		extra["met"], extra["evidence"] = true, "verified"
		return extra
	}
	for _, bad := range [][]map[string]any{
		{met(map[string]any{"number": 1}), met(map[string]any{"number": 3})},
		{met(map[string]any{"number": 1}), met(map[string]any{"number": 2, "criterion": "PR 合并到 master"})},
		{met(map[string]any{"number": 1})},
	} {
		if res, err := review(bad...); err == nil {
			t.Fatalf("accepted %v: %s", bad, res)
		}
	}
	if cur := todoNow(s, todo.ID); cur.DoneAt != nil || len(cur.Supervision.Reviews) != 0 {
		t.Fatalf("a refused review changed the task: %+v", cur.Supervision)
	}
	res, err := review(met(map[string]any{"number": 1, "criterion": "PR 合并到 master"}), met(map[string]any{"number": 2}))
	if err != nil || !strings.Contains(res, "accepted and closed") || !strings.Contains(res, "1. ") {
		t.Fatalf("accept by number: %s %v", res, err)
	}
	cur := todoNow(s, todo.ID)
	if got := cur.Supervision.Reviews[0].Checks; got[1].Criterion != "浅色 / 深色截图贴进 PR" || got[1].Number != 0 {
		t.Fatalf("stored checks: %+v", got)
	}
}

func TestTaskScopeOperations(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	scope := func(args map[string]any) (string, error) {
		args["todo_id"], args["change"], args["why"] = todo.ID, "tighten wording", "clearer"
		return runJSON(t, s, "conv1", "task_scope", args)
	}
	// A reading on criterion 1 survives an edit of its wording, which is no drop.
	// (The rework's send fails: the task has no live agent here. The reading is recorded all the same.)
	_, _ = runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "rework", "note": "second one missing",
		"checks": []map[string]any{{"number": 1, "met": true, "evidence": "pull/901 merged"}}})
	res, err := scope(map[string]any{"edit": []map[string]any{{"number": 1, "text": "PR 合并到 master (squash)"}}})
	if err != nil || !strings.Contains(res, "1. \"PR 合并到 master (squash)\"") && !strings.Contains(res, "1. PR 合并到 master (squash)") || !strings.Contains(res, "Dropped criteria: none") {
		t.Fatalf("edit: %s %v", res, err)
	}
	v := todoNow(s, todo.ID).Supervision
	if v.DoneWhen[0] != "PR 合并到 master (squash)" || v.DoneWhen[1] != "浅色 / 深色截图贴进 PR" || v.Criteria[0].State != "met" || v.Criteria[0].Evidence != "pull/901 merged" {
		t.Fatalf("ledger after edit: %+v", v)
	}
	b, _ := s.briefOfTodo(todo.ID)
	if last := b.Log[len(b.Log)-1]; len(last.Edited) != 1 || last.Edited[0].From != "PR 合并到 master" || len(last.Dropped) != 0 || len(last.Added) != 0 {
		t.Fatalf("event: %+v", last)
	}
	if got := briefFromView(b).dropped(); len(got) != 0 {
		t.Fatalf("edit recorded as dropped: %+v", got)
	}
	// add and drop, by number; both forms together are refused.
	if res, err := scope(map[string]any{"add": []string{"docs updated"}, "drop": []int{2}}); err != nil || !strings.Contains(res, "2. docs updated") {
		t.Fatalf("add/drop: %s %v", res, err)
	}
	if got := todoNow(s, todo.ID).Supervision.DoneWhen; len(got) != 2 || got[1] != "docs updated" {
		t.Fatalf("after add/drop: %v", got)
	}
	if b, _ = s.briefOfTodo(todo.ID); len(briefFromView(b).dropped()) != 1 {
		t.Fatalf("a real drop is recorded: %+v", briefFromView(b).dropped())
	}
	if _, err := scope(map[string]any{"add": []string{"x"}, "done_when": []string{"y"}}); err == nil {
		t.Fatal("whole list with operations")
	}
	if _, err := scope(map[string]any{"drop": []int{7}}); err == nil {
		t.Fatal("number out of range")
	}
	// Only the estimate changed: one line, no criteria.
	res, err = scope(map[string]any{"expect_minutes": 90})
	if err != nil || strings.Contains(res, "criteria") || strings.Contains(res, "\n") || !strings.Contains(res, "expect_minutes 90") {
		t.Fatalf("estimate only: %q %v", res, err)
	}
}

// 22:42:19: the wording of a criterion is never evidence about the result.
func TestAcceptGuardIgnoresCriterionWording(t *testing.T) {
	s, f := newQuietService(t)
	f.judge = `{"verdict":"done","reason":"ok"}`
	list := []string{"登录页不再崩溃", "补了横屏测试", "PR 合并到 master", "仅当督办判定需要用户决定时才升级给用户"}
	if _, err := runJSON(t, s, "conv1", "task_plan", briefTask(map[string]any{"done_when": list})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent", func() bool { return len(onlyTodo(s).Agents) == 1 })
	todo := onlyTodo(s)
	checks := make([]ReviewCheck, 0, len(list))
	for _, c := range list {
		checks = append(checks, ReviewCheck{Criterion: c, Met: true, Evidence: "verified"})
	}
	res, err := runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": "all four checked", "checks": checks})
	if err != nil || !strings.Contains(res, "accepted and closed") {
		t.Fatalf("criterion wording refused the acceptance: %s %v", res, err)
	}
}

// 23:05:59: the worker's own Status line disagrees with criteria that are all met.
func TestAcceptGuardWorkerStatusAsksForANote(t *testing.T) {
	s, _, todo := guardedReviewTask(t, "### Persona 工作报告\n- Status: blocked\n- Did: everything asked, one extra thing is outside the criteria")
	review := func(note string) (string, error) {
		return runJSON(t, s, "conv1", "task_review", map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": note, "checks": metChecks()})
	}
	if _, err := review(""); err == nil || !strings.Contains(err.Error(), "note") {
		t.Fatalf("without a note: %v", err)
	}
	if cur := todoNow(s, todo.ID); cur.DoneAt != nil || len(cur.Supervision.Reviews) != 0 {
		t.Fatalf("a refused review changed the task: %+v", cur.Supervision)
	}
	res, err := review("The worker left the old flag cleanup outside the criteria.")
	if err != nil || !strings.Contains(res, "accepted and closed") || !strings.Contains(res, "Status disagreed") {
		t.Fatalf("with a note: %s %v", res, err)
	}
}

// A task leader names checks by number and changes a worker's criteria with the same operations.
func TestTaskLeaderChecksByNumberAndScopeOperations(t *testing.T) {
	s, f := newQuietService(t)
	startTaskLeader(t, s, f)
	w := planWorker(t, s, f, nil)
	before := todoNow(s, w.ID).Supervision.DoneWhen
	if res := leaderCall(s, "task_scope", map[string]any{"todo_id": w.ID, "change": "tighten", "why": "clearer",
		"edit": []map[string]any{{"number": 1, "text": before[0] + " (verified)"}}}); res.IsError {
		t.Fatalf("leader edit: %+v", res)
	}
	now := todoNow(s, w.ID).Supervision.DoneWhen
	if now[0] != before[0]+" (verified)" || now[1] != before[1] {
		t.Fatalf("criteria: %v", now)
	}
	if res := leaderCall(s, "task_scope", map[string]any{"todo_id": w.ID, "change": "x", "why": "y", "add": []string{strings.Repeat("z", 201)}}); !res.IsError {
		t.Fatalf("a long criterion was accepted: %+v", res)
	}
	checks := []map[string]any{{"number": 2, "met": true, "evidence": "ok"}}
	if res := leaderCall(s, "task_review", map[string]any{"todo_id": w.ID, "verdict": "accepted", "checks": checks}); !res.IsError || todoNow(s, w.ID).DoneAt != nil {
		t.Fatalf("an incomplete review: %+v", res)
	}
	checks = append(checks, map[string]any{"number": 1, "met": true, "evidence": "ok"})
	if res := leaderCall(s, "task_review", map[string]any{"todo_id": w.ID, "verdict": "accepted", "checks": checks}); res.IsError || todoNow(s, w.ID).DoneAt == nil {
		t.Fatalf("accept by number: %+v", res)
	}
}
