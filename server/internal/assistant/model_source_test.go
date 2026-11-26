package assistant

import (
	"context"
	"testing"

	"github.com/sunfayao/persona/server/internal/modelhttp"
)

func TestScheduledModelSourceIncludesBothIDs(t *testing.T) {
	if got := planModelSource(PlanTask{TodoID: "todo_123", Schedule: "routine_456"}); got != (modelhttp.Source{Role: "task_agent", Todo: "todo_123", Routine: "routine_456"}) {
		t.Fatalf("scheduled work Agent source=%+v", got)
	}
	s := newTestService(t, newFakeHub().withMachine())
	task := Task{ID: "routine_123", TodoID: "todo_456"}
	start := taskStartParams(task, "example prompt", TaskExecution{})
	ctx := context.WithValue(context.Background(), modelSourceKey{}, start.modelSource)
	want := modelhttp.Source{Role: "routine", Todo: task.TodoID, Routine: task.ID}
	if got := s.startModelSource(ctx, StartParams{}, false); got != want {
		t.Fatalf("start source=%+v", got)
	}
	if got := taskResumeParams(task, "example prompt", TaskExecution{})["model_source"]; got != want {
		t.Fatalf("resume source=%+v", got)
	}
	if got := s.startModelSource(context.Background(), StartParams{Origin: OriginInbox}, false); got.Role != "background" {
		t.Fatal("Inbox start not background")
	}
	if got := s.startModelSource(context.Background(), StartParams{Purpose: purposeSupervise}, false); got.Role != "background" {
		t.Fatal("supervisor start not background")
	}
}

func TestSendParamsRejectsCallerModelSource(t *testing.T) {
	f := newFakeHub().withMachine()
	f.settings = &Settings{MachineID: "server"}
	s := newTestService(t, f)
	s.originSet(t.Context(), "server", "inbox", OriginInbox, "example:item", "")
	p := map[string]any{"model_source": modelhttp.Source{Role: "main", Todo: "todo_forged"}}
	params, _, err := s.sendParamsOwn(t.Context(), SendParams{MachineID: "server", SessionID: "inbox", Params: p})
	if err != nil {
		t.Fatal(err)
	}
	if got := params["model_source"]; got != (modelhttp.Source{Role: "background", Session: "inbox"}) {
		t.Fatalf("caller source trusted: %+v", got)
	}
}
