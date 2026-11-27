package assistant

import (
	"testing"
)

func TestScheduledTodoTaskLeaderEscalation(t *testing.T) {
	for _, supervisor := range []string{"conv1", "lead1"} {
		t.Run(supervisor, func(t *testing.T) {
			s, _, worker := scheduledTodoFixture(t)
			ctx := t.Context()
			leader := Todo{ID: "task-leader", Origin: supervisor,
				Agents:      []TodoAgent{{MachineID: "box", SessionID: "leader", Source: sourceLeader}},
				Supervision: &Supervision{Supervisor: supervisor}}
			putTodo(s, leader)
			worker.Origin = leaderRef("box", "leader")
			worker.Supervision.Supervisor = worker.Origin
			putTodo(s, worker)
			if !s.agentSupervised(worker) || !s.mayReviewForLeader(ctx, supervisor, worker) {
				t.Fatal("the fixture is not a worker beneath the supervising conversation")
			}
			task := todoProgressTask(worker.ID)
			setTasks(s, task)
			assessTodoBaseline(t, s, task.ID)
			_, first := assessTodoRun(t, s, task.ID, nil)
			if first.State != "no_progress" || first.NoProgress != 1 {
				t.Fatalf("first worker assessment: %+v", first)
			}
			_, second := assessTodoRun(t, s, task.ID, nil)
			current, _ := taskByID(s, task.ID)
			cur, _ := s.todoNow(worker.ID)
			if second.State != "no_progress" || second.NoProgress != 2 || !current.TodoProgress.Escalated ||
				cur.Supervision.State != reviewNeedsUser || len(cur.Supervision.Reviews) != 1 {
				t.Fatalf("the worker did not escalate: %+v %+v", current.TodoProgress, cur.Supervision)
			}
			if cur.Supervision.Supervisor != worker.Origin || cur.DoneAt != nil {
				t.Fatalf("escalation took over or closed the worker: %+v", cur)
			}
			reviews := 0
			for _, event := range testEvents(s) {
				if event.Kind == taskReviewedEvent && event.Detail["todo"] == worker.ID {
					reviews++
					if event.Detail["conversation"] != supervisor {
						t.Fatalf("the decision used an Agent reference instead of the conversation above it: %+v", event.Detail)
					}
				}
			}
			if reviews != 1 {
				t.Fatalf("expected one review by the supervising conversation, got %d", reviews)
			}
			ask := askOf(s, worker.ID)
			if ask == nil || !decideAsk(ask) || len(ask.Options) != 2 {
				t.Fatalf("the worker lacks the existing actionable owner decision: %+v", ask)
			}
			assessTodoRun(t, s, task.ID, nil)
			cur, _ = s.todoNow(worker.ID)
			if len(cur.Supervision.Reviews) != 1 {
				t.Fatal("a later no-progress run repeated the worker's owner decision")
			}
		})
	}
}

func TestScheduledTodoEscalationIdentityResetsWithProgress(t *testing.T) {
	s, _, todo := scheduledTodoFixture(t)
	task := todoProgressTask(todo.ID)
	setTasks(s, task)
	assessTodoBaseline(t, s, task.ID)
	assessTodoRun(t, s, task.ID, nil)
	first, _ := assessTodoRun(t, s, task.ID, nil)
	assessTodoRun(t, s, task.ID, func() {
		cur, _ := s.todoNow(todo.ID)
		cur.Supervision.State = ""
		cur.Supervision.Criteria[0].State = "met"
		putTodo(s, cur)
	})
	current, _ := taskByID(s, task.ID)
	if scheduleJSON(t, current.TodoProgress)["escalation_run"] != nil || current.TodoProgress.Escalated {
		t.Fatal("observable progress retained the prior escalation's identity")
	}
	assessTodoRun(t, s, task.ID, nil)
	second, _ := assessTodoRun(t, s, task.ID, nil)
	cur, _ := s.todoNow(todo.ID)
	if len(cur.Supervision.Reviews) != 2 || first.ID == second.ID {
		t.Fatal("a new no-progress streak did not create its own decision")
	}
	current, _ = taskByID(s, task.ID)
	if scheduleJSON(t, current.TodoProgress)["escalation_run"] != second.ID {
		t.Fatal("a new streak reused the previous escalation's identity")
	}
}
