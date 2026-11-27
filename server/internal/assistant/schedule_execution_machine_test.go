package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

func TestRecurringTaskKeepsItsConversationDuringLauncherDiscoveryFailure(t *testing.T) {
	f := readyHub()
	s := newTestService(t, f)
	setTasks(s, Task{ID: "job", Title: "Check", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().UTC(), Enabled: true})
	setModel(t, s, "task.job", ModelBinding{Provider: "launcher.codex", Model: "task-model"})

	catalog := func(baseURL string) {
		f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
			return map[string]any{"launchers": []proto.Launcher{{Name: "codex", Agent: "codex", Source: "zshrc", BaseURL: baseURL}}}, nil
		})
	}
	run := func(number int) ScheduleRun {
		t.Helper()
		task, _ := taskByID(s, "job")
		s.runDue(t.Context(), task.At)
		s.sched.inflight.Wait()
		got := waitRuns(t, s, number)[number-1]
		if got.Manual || got.Status != runOK || got.Execution == nil || got.SessionID == "" {
			t.Fatalf("scheduled run %d did not dispatch: %+v", number, got)
		}
		return got
	}
	assertResumed := func(got, first ScheduleRun) {
		t.Helper()
		send := f.lastSend(t)
		if got.SessionID != first.SessionID || got.Conversation != "resumed" || got.ConversationReason != "" ||
			send["session_id"] != first.SessionID || send["resume_only"] != true || send["task_execution"] != true ||
			send["model"] != got.Execution.Model {
			t.Fatalf("scheduled run replaced its own conversation %s: %+v (fingerprint %s), send %v",
				first.SessionID, got, got.Execution.Fingerprint, send)
		}
	}
	assertReplaced := func(got, previous ScheduleRun) {
		t.Helper()
		if got.SessionID == previous.SessionID || got.Conversation != "new" || got.ConversationReason != "execution_changed" {
			t.Fatalf("changed execution reused %s: %+v", previous.SessionID, got)
		}
	}

	catalog("https://first.example")
	first := run(1)
	if first.Execution.Fingerprint == "" || first.Execution.BindingFingerprint == "" {
		t.Fatalf("missing observed execution: %+v", first.Execution)
	}
	f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
		return nil, errors.New("synthetic catalog unavailable")
	})
	outage := run(2)
	assertResumed(outage, first)
	stored, _ := taskByID(s, "job")
	if outage.Execution.Fingerprint != "" || outage.Execution.BindingFingerprint != first.Execution.BindingFingerprint ||
		stored.RunExecution.Fingerprint != first.Execution.Fingerprint {
		t.Fatalf("outage changed saved evidence: current=%+v saved=%+v", outage.Execution, stored.RunExecution)
	}
	legacy := *stored.RunExecution
	legacy.BindingFingerprint = "" // the previous release stored only the combined digest
	stored.RunExecution = &legacy
	setTasks(s, stored)
	f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
		return nil, context.DeadlineExceeded
	})
	assertResumed(run(3), first)
	f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
		return map[string]any{"launchers": []proto.Launcher{{Name: "codex", Agent: "codex", Source: "builtin"}}}, nil
	})
	assertResumed(run(4), first)
	catalog("https://first.example")
	assertResumed(run(5), first)
	catalog("https://second.example")
	changed := run(6)
	assertReplaced(changed, first)
	if changed.Execution.Fingerprint == first.Execution.Fingerprint || changed.Execution.BindingFingerprint != first.Execution.BindingFingerprint {
		t.Fatalf("launcher change fingerprints: before=%+v after=%+v", first.Execution, changed.Execution)
	}
	f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
		return nil, errors.New("synthetic catalog unavailable")
	})
	setModel(t, s, "task.job", ModelBinding{Provider: "launcher.codex", Model: "replacement-model"})
	newModel := run(7)
	assertReplaced(newModel, changed)
	if newModel.Execution.BindingFingerprint == changed.Execution.BindingFingerprint {
		t.Fatalf("model change did not change its binding fingerprint: %+v", newModel.Execution)
	}
	stored, _ = taskByID(s, "job")
	legacy = *stored.RunExecution
	legacy.Fingerprint, legacy.BindingFingerprint = legacy.BindingFingerprint, ""
	stored.RunExecution = &legacy // an older run whose launcher identity was unavailable
	setTasks(s, stored)
	catalog("https://second.example")
	assertResumed(run(8), newModel)
	catalog("https://third.example")
	assertReplaced(run(9), newModel)
}

func TestTaskExecutionRuntimeReusesScheduledAndManualRuns(t *testing.T) {
	f := newFakeHub().withMachine()
	f.launcherList = []proto.Launcher{{Name: "dsh", Agent: "dsh", Source: "dsh", Assistant: true}}
	s := newTestService(t, f)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.dsh"})
	setTasks(s, Task{ID: "job", Title: "Check", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
	starts, sends := 0, 0
	unknown := false
	f.answerNode(DeviceAssistantSend, func(_ string, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		sends++
		if id, _ := p["session_id"].(string); id == "" {
			starts++
		} else if id != "runtime-task" || p["resume_only"] != true || p["task_execution"] != true {
			t.Errorf("runtime task lost its strict resume: %v", p)
		}
		if unknown {
			return nil, rpc.ErrClosed
		}
		return map[string]string{"session_id": "runtime-task"}, nil
	})
	for range 2 {
		if _, err := runNow(t, s, "job"); err != nil {
			t.Fatal(err)
		}
	}
	if r := request(s, "POST", "/api/v1/assistant/tasks/job/run", `{}`); r.Code != 200 {
		t.Fatalf("manual runtime resume: %d %s", r.Code, r.Body.String())
	}
	unknown = true
	if _, err := runNow(t, s, "job"); err == nil || starts != 1 || sends != 4 {
		t.Fatalf("unknown runtime delivery replaced or retried: %v starts=%d sends=%d", err, starts, sends)
	}
}
