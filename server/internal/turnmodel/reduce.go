package turnmodel

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Snapshot contains the canonical plan and timers and their legacy session projections.
// Background is in start order; BackgroundEnded is in end order, with at most eight entries.
// Timers are in scheduling order, with Loop projecting the last one.
type Snapshot struct {
	Plan            *proto.PlanInfo
	PlanEntries     []proto.PlanEntry
	Background      []proto.BackgroundTask
	BackgroundEnded []proto.BackgroundTask
	Timers          []proto.TaskFacet
	timersSeen      bool
	Loop            *proto.LoopInfo
	Subagents       []proto.Subagent
	Artifacts       []proto.ArtifactFacet
	Progress        *proto.SessionProgress
	Turn            *proto.TurnState
}

type backgroundTask struct {
	order int
	task  proto.BackgroundTask
}

type timerTask struct {
	order int
	task  proto.TaskFacet
}

// Reduce projects facets from items in stream order; legacy fields and unrecognized modes, kinds and statuses do
// not change the snapshot. It uses only item timestamps and leaves the input unchanged.
func Reduce(items []proto.Item) Snapshot {
	var out Snapshot
	var planAt time.Time
	background := make(map[string]backgroundTask)
	timers := make(map[string]timerTask)
	fired := make(map[string]int)
	subagents := make(map[string]int)
	artifacts := make(map[string]int)
	for i, item := range items {
		if item.Facets == nil {
			continue
		}
		out.observePresentation(item, subagents, artifacts)
		if plan := item.Facets.Plan; plan != nil {
			switch plan.Mode {
			case proto.TurnPlanModeReplace:
				out.PlanEntries = append(out.PlanEntries[:0], plan.Entries...)
				if out.PlanEntries == nil {
					out.PlanEntries = []proto.PlanEntry{}
				}
				planAt = item.TS
			case proto.TurnPlanModeMerge:
				out.PlanEntries = mergePlan(out.PlanEntries, plan.Entries)
				planAt = item.TS
			}
		}
		if notice := item.Facets.Notice; notice != nil && notice.Code == proto.NoticeCodeServerRestart {
			out.timersSeen = true
			clear(timers)
			clear(fired)
			for _, running := range slices.SortedFunc(maps.Values(background), func(a, b backgroundTask) int { return cmp.Compare(a.order, b.order) }) {
				task := proto.TaskFacet{ID: running.task.ID, Kind: running.task.Kind, Status: proto.TaskStatusCancelled}
				out.BackgroundEnded = updateBackground(background, out.BackgroundEnded, i, item.TS, task)
			}
		}

		if task := item.Facets.Task; task != nil {
			switch task.Kind {
			case proto.TaskKindTimer:
				out.timersSeen = true
				updateTimer(timers, fired, i, *task)
			case proto.TaskKindShell, proto.TaskKindMonitor, proto.TaskKindAgent, proto.TaskKindWorkflow:
				out.BackgroundEnded = updateBackground(background, out.BackgroundEnded, i, item.TS, *task)
			}
		}
	}
	if out.PlanEntries != nil {
		out.Plan = projectPlan(out.PlanEntries, planAt)
	}
	for _, active := range slices.SortedFunc(maps.Values(background), func(a, b backgroundTask) int {
		return cmp.Compare(a.order, b.order)
	}) {
		out.Background = append(out.Background, active.task)
	}
	for _, active := range slices.SortedFunc(maps.Values(timers), func(a, b timerTask) int {
		return cmp.Compare(a.order, b.order)
	}) {
		task := active.task
		if task.Timer != nil {
			task.Timer = new(*task.Timer)
		}
		if task.ExitCode != nil {
			task.ExitCode = new(*task.ExitCode)
		}
		out.Timers = append(out.Timers, task)
	}
	if len(out.Timers) > 0 {
		latest := out.Timers[len(out.Timers)-1]
		out.Loop = projectLoop(latest.Timer, fired[latest.ID])
	}
	return out
}

func mergePlan(entries, updates []proto.PlanEntry) []proto.PlanEntry {
	if entries == nil {
		entries = []proto.PlanEntry{}
	}
	index := make(map[string]int, len(entries))
	for i, entry := range entries {
		if entry.ID != "" {
			index[entry.ID] = i
		}
	}
	for _, entry := range updates {
		if i, ok := index[entry.ID]; ok {
			entries[i] = entry
			continue
		}
		if entry.ID != "" {
			index[entry.ID] = len(entries)
		}
		entries = append(entries, entry)
	}
	return entries
}

func projectPlan(entries []proto.PlanEntry, at time.Time) *proto.PlanInfo {
	p := &proto.PlanInfo{At: at}
	current := false
	for _, entry := range entries {
		if entry.Status == proto.PlanEntryStatusCancelled {
			continue
		}
		p.Total++
		switch entry.Status {
		case proto.PlanEntryStatusCompleted, proto.PlanEntryStatusSkipped:
			p.Done++
		case proto.PlanEntryStatusInProgress:
			if !current {
				current = true
				p.Current = entry.ActiveForm
				if p.Current == "" {
					p.Current = entry.Title
				}
			}
		}
	}
	return p
}

func updateBackground(active map[string]backgroundTask, ended []proto.BackgroundTask, order int, at time.Time, facet proto.TaskFacet) []proto.BackgroundTask {
	work, ok := active[facet.ID]
	if !ok {
		work = backgroundTask{order: order, task: proto.BackgroundTask{ID: facet.ID, StartedAt: at}}
	}
	work.task.Kind = facet.Kind
	if facet.Title != "" {
		work.task.Description = facet.Title
	}
	if facet.Command != "" {
		work.task.Command = facet.Command
	}
	if facet.OutputFile != "" {
		work.task.OutputFile = facet.OutputFile
	}
	switch facet.Status {
	case proto.TaskStatusRunning, proto.TaskStatusWaiting, proto.TaskStatusEvent:
		active[facet.ID] = work
	case proto.TaskStatusCompleted, proto.TaskStatusFailed, proto.TaskStatusCancelled:
		if !ok {
			// A command can be stopped by a control call and then confirmed by a native notice.
			for i := len(ended) - 1; i >= 0; i-- {
				if ended[i].ID == facet.ID {
					work.task.StartedAt = ended[i].StartedAt
					work.task.Command = cmp.Or(work.task.Command, ended[i].Command)
					work.task.Description = cmp.Or(work.task.Description, ended[i].Description)
					work.task.OutputFile = cmp.Or(work.task.OutputFile, ended[i].OutputFile)
					work.task.ExitCode = ended[i].ExitCode
					ended = slices.Delete(ended, i, i+1)
					break
				}
			}
		}
		delete(active, facet.ID)
		work.task.Status = facet.Status
		work.task.EndedAt = &at
		if facet.ExitCode != nil {
			work.task.ExitCode = new(*facet.ExitCode)
		}
		if len(ended) == 8 {
			copy(ended, ended[1:])
			ended[7] = work.task
		} else {
			ended = append(ended, work.task)
		}
	}
	return ended
}

func updateTimer(active map[string]timerTask, fired map[string]int, order int, facet proto.TaskFacet) {
	switch facet.Status {
	case proto.TaskStatusScheduled:
		active[facet.ID] = timerTask{order: order, task: facet}
	case proto.TaskStatusFired:
		fired[facet.ID]++
		timer, ok := active[facet.ID]
		if !ok {
			return
		}
		if facet.Timer == nil {
			facet.Timer = timer.task.Timer
		}
		spec := facet.Timer
		if spec == nil || spec.IntervalS <= 0 && spec.Cron == "" || spec.MaxRuns != 0 && fired[facet.ID] >= spec.MaxRuns {
			delete(active, facet.ID)
			return
		}
		timer.task = facet
		active[facet.ID] = timer
	case proto.TaskStatusCancelled:
		delete(active, facet.ID)
	}
}

func projectLoop(spec *proto.TimerSpec, ticks int) *proto.LoopInfo {
	loop := &proto.LoopInfo{Kind: "wakeup", Ticks: ticks}
	if spec != nil {
		loop.Prompt = spec.Prompt
		loop.IntervalSeconds = spec.IntervalS
		loop.NextAt = spec.NextAt
		loop.Cron = spec.Cron
		if spec.Cron != "" {
			loop.Kind = "cron"
		}
	}
	return loop
}
