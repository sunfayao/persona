package turnmodel

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func at(n int) time.Time { return time.Unix(int64(n), 0).UTC() }

func planItem(n int, mode string, entries ...proto.PlanEntry) proto.Item {
	return proto.Item{TS: at(n), Facets: &proto.ItemFacets{Plan: &proto.PlanFacet{Mode: mode, Entries: entries}}}
}

func taskItem(n int, task proto.TaskFacet) proto.Item {
	return proto.Item{TS: at(n), Facets: &proto.ItemFacets{Task: &task}}
}

func TestReduceReplacesAndMergesPlansInStreamOrder(t *testing.T) {
	t.Parallel()
	read := proto.PlanEntry{ID: "read", Title: "Read the example", Status: proto.PlanEntryStatusPending}
	readDone := read
	readDone.Status = proto.PlanEntryStatusCompleted
	write := proto.PlanEntry{ID: "write", Title: "Write the test", Status: proto.PlanEntryStatusInProgress, ActiveForm: "Writing the test"}
	review := proto.PlanEntry{Title: "Review the test", Status: proto.PlanEntryStatusInProgress}
	skip := proto.PlanEntry{Title: "Optional check", Status: proto.PlanEntryStatusSkipped}
	cancel := proto.PlanEntry{ID: "cancel", Title: "Publish the example", Status: proto.PlanEntryStatusCancelled}
	fail := proto.PlanEntry{ID: "fail", Title: "Try the build", Status: proto.PlanEntryStatusFailed}
	items := []proto.Item{
		{Task: &proto.TaskNote{Kind: "shell", Status: "running"}},
		planItem(2, proto.TurnPlanModeReplace, read, write, cancel),
		planItem(3, proto.TurnPlanModeMerge, readDone, review, skip, fail),
		planItem(4, "future_mode", cancel),
		planItem(5, proto.TurnPlanModeReplace, review),
		planItem(6, proto.TurnPlanModeReplace),
	}
	for _, c := range []struct {
		name    string
		through int
		plan    *proto.PlanInfo
		entries []proto.PlanEntry
	}{
		{"legacy item is ignored", 1, nil, nil},
		{"replace sets the plan", 2, &proto.PlanInfo{Total: 2, Current: write.ActiveForm, At: at(2)}, []proto.PlanEntry{read, write, cancel}},
		{"merge upserts by ID and appends distinct anonymous entries", 3,
			&proto.PlanInfo{Done: 2, Total: 5, Current: write.ActiveForm, At: at(3)}, []proto.PlanEntry{readDone, write, cancel, review, skip, fail}},
		{"unknown mode does not clear the plan", 4,
			&proto.PlanInfo{Done: 2, Total: 5, Current: write.ActiveForm, At: at(3)}, []proto.PlanEntry{readDone, write, cancel, review, skip, fail}},
		{"replace drops entries and falls back to the current title", 5, &proto.PlanInfo{Total: 1, Current: review.Title, At: at(5)}, []proto.PlanEntry{review}},
		{"empty replace clears an existing plan", 6, &proto.PlanInfo{At: at(6)}, []proto.PlanEntry{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want := Snapshot{Plan: c.plan, PlanEntries: c.entries}
			if got := Reduce(items[:c.through]); !reflect.DeepEqual(got, want) {
				t.Errorf("Reduce() plan = %+v, entries = %+v; want %+v, %+v", got.Plan, got.PlanEntries, c.plan, c.entries)
			}
		})
	}
}

func TestReduceRetainsBackgroundMetadataAndBoundsEndedTasks(t *testing.T) {
	t.Parallel()
	zero := 0
	items := []proto.Item{
		taskItem(1, proto.TaskFacet{ID: "z", Kind: proto.TaskKindShell, Status: proto.TaskStatusRunning, Title: "Build", Command: "go build", OutputFile: "build.log"}),
		taskItem(2, proto.TaskFacet{ID: "m", Kind: proto.TaskKindMonitor, Status: proto.TaskStatusWaiting, Title: "Watch the build"}),
		taskItem(3, proto.TaskFacet{ID: "a", Kind: proto.TaskKindAgent, Status: proto.TaskStatusRunning, Title: "Review"}),
		taskItem(4, proto.TaskFacet{ID: "z", Kind: proto.TaskKindShell, Status: proto.TaskStatusCompleted, ExitCode: &zero}),
		taskItem(5, proto.TaskFacet{ID: "m", Kind: proto.TaskKindMonitor, Status: proto.TaskStatusEvent}),
		taskItem(6, proto.TaskFacet{ID: "m", Kind: proto.TaskKindMonitor, Status: "future_status"}),
		taskItem(7, proto.TaskFacet{ID: "m", Kind: "future_kind", Status: proto.TaskStatusFailed}),
	}
	shell := proto.BackgroundTask{ID: "z", Kind: proto.TaskKindShell, Description: "Build", Command: "go build", OutputFile: "build.log", StartedAt: at(1)}
	monitor := proto.BackgroundTask{ID: "m", Kind: proto.TaskKindMonitor, Description: "Watch the build", StartedAt: at(2)}
	agent := proto.BackgroundTask{ID: "a", Kind: proto.TaskKindAgent, Description: "Review", StartedAt: at(3)}
	var workflowEnded []proto.BackgroundTask
	for i := range 9 {
		n := 10 + i
		id := "job-" + strconv.Itoa(i)
		status := proto.TaskStatusFailed
		if i%2 == 0 {
			status = proto.TaskStatusCancelled
		}
		items = append(items, taskItem(n, proto.TaskFacet{ID: id, Kind: proto.TaskKindWorkflow, Status: status}))
		end := at(n)
		workflowEnded = append(workflowEnded, proto.BackgroundTask{ID: id, Kind: proto.TaskKindWorkflow, StartedAt: end, EndedAt: &end, Status: status})
	}
	finished := shell
	end := at(4)
	finished.Status, finished.EndedAt, finished.ExitCode = proto.TaskStatusCompleted, &end, &zero
	for _, c := range []struct {
		name    string
		through int
		active  []proto.BackgroundTask
		ended   []proto.BackgroundTask
	}{
		{"active tasks are ordered by first sighting", 3, []proto.BackgroundTask{shell, monitor, agent}, nil},
		{"terminal update preserves the start and a zero exit code", 7, []proto.BackgroundTask{monitor, agent}, []proto.BackgroundTask{finished}},
		{"only the last eight outcomes remain in end order", len(items), []proto.BackgroundTask{monitor, agent}, workflowEnded[1:]},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want := Snapshot{Background: c.active, BackgroundEnded: c.ended}
			if got := Reduce(items[:c.through]); !reflect.DeepEqual(got, want) {
				t.Errorf("Reduce() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestReduceProjectsTheNewestActiveTimer(t *testing.T) {
	t.Parallel()
	wakeup := proto.TaskFacet{ID: "wake", Kind: proto.TaskKindTimer, Status: proto.TaskStatusScheduled,
		Timer: &proto.TimerSpec{Prompt: "Check status", IntervalS: 60, MaxRuns: 2, NextAt: at(61)}}
	cron := proto.TaskFacet{ID: "cron", Kind: proto.TaskKindTimer, Status: proto.TaskStatusScheduled,
		Timer: &proto.TimerSpec{Cron: "0 * * * *", Prompt: "Check the queue", NextAt: at(3600)}}
	once := proto.TaskFacet{ID: "once", Kind: proto.TaskKindTimer, Status: proto.TaskStatusScheduled, Timer: &proto.TimerSpec{NextAt: at(5)}}
	firedWake := wakeup
	firedWake.Status = proto.TaskStatusFired
	firedWake.Timer = &proto.TimerSpec{Prompt: "Check status", IntervalS: 60, MaxRuns: 2, NextAt: at(121)}
	firedCron := cron
	firedCron.Status = proto.TaskStatusFired
	items := []proto.Item{
		taskItem(1, wakeup), taskItem(2, cron), taskItem(3, once),
		taskItem(5, proto.TaskFacet{ID: once.ID, Kind: proto.TaskKindTimer, Status: proto.TaskStatusFired}),
		taskItem(61, firedWake),
		taskItem(62, proto.TaskFacet{ID: cron.ID, Kind: proto.TaskKindTimer, Status: proto.TaskStatusFired}),
		taskItem(63, proto.TaskFacet{ID: cron.ID, Kind: proto.TaskKindTimer, Status: proto.TaskStatusCancelled}),
		taskItem(121, proto.TaskFacet{ID: wakeup.ID, Kind: proto.TaskKindTimer, Status: proto.TaskStatusFired}),
	}
	for _, c := range []struct {
		name    string
		through int
		timers  []proto.TaskFacet
		loop    *proto.LoopInfo
	}{
		{"scheduled one-shot is the newest", 3, []proto.TaskFacet{wakeup, cron, once}, &proto.LoopInfo{Kind: "wakeup", NextAt: at(5)}},
		{"one-shot disappears when fired; a tick does not reorder timers", 5, []proto.TaskFacet{firedWake, cron},
			&proto.LoopInfo{Kind: "cron", Prompt: cron.Timer.Prompt, Cron: cron.Timer.Cron, NextAt: at(3600)}},
		{"recurring cron retains its spec and counts ticks", 6, []proto.TaskFacet{firedWake, firedCron},
			&proto.LoopInfo{Kind: "cron", Prompt: cron.Timer.Prompt, Cron: cron.Timer.Cron, NextAt: at(3600), Ticks: 1}},
		{"cancellation falls back to the remaining interval timer", 7, []proto.TaskFacet{firedWake},
			&proto.LoopInfo{Kind: "wakeup", Prompt: wakeup.Timer.Prompt, IntervalSeconds: 60, NextAt: at(121), Ticks: 1}},
		{"max runs removes the last timer", 8, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want := Snapshot{Timers: c.timers, timersSeen: true, Loop: c.loop}
			if got := Reduce(items[:c.through]); !reflect.DeepEqual(got, want) {
				t.Errorf("Reduce() timers = %+v, loop = %+v; want %+v, %+v", got.Timers, got.Loop, c.timers, c.loop)
			}
		})
	}
}
