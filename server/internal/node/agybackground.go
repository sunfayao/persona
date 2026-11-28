package node

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
)

const (
	agyTaskPrefix           = "Tool is running as a background task with task id:"
	agyBackgroundEndedLimit = 8
)

var (
	agyTaskName      = lazyre.New(`^task-[0-9]+$`)
	agyTaskEnd       = lazyre.New(`(?i)\bTask id "([^"\n]+)" (?:was )?(finished|failed|canceled|cancelled|killed|stopped)\b`)
	agyServerRestart = lazyre.New(`(?i)\bAll your subagents and background tasks have been stopped due to server restart\b`)
	agyMsgHeader     = lazyre.New(`\[Message\]([^\n]*)`)
	agyMsgSender     = lazyre.New(`\bsender=([^\s]+)`)
	agyMsgTimestamp  = lazyre.New(`\btimestamp=([^\s]+)`)
	agyTaskCancelled = lazyre.New(`\bTask "([^"\n]+)" cancelled\.`)
	agyTaskStatus    = lazyre.New(`(?m)^Task:\s*(\S+)\s*\nStatus:\s*(\w+)\b`)
)

func agyBackgroundNotice(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "Created At:") {
		_, content, _ = strings.Cut(content, "\n")
	}
	if content, ok := strings.CutPrefix(strings.TrimSpace(content), agyTaskPrefix); ok {
		return content
	}
	return ""
}

func agyToolString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	// Native exports can encode a tool argument as JSON inside a string.
	var decoded string
	if strings.HasPrefix(value, `"`) && json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func agyToolInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	if s := strings.TrimSpace(agyToolString(raw)); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			return v
		}
	}
	return 0
}

func agyParseTimerDesc(desc string) time.Duration {
	rest, ok := strings.CutPrefix(strings.TrimSpace(desc), "Timer:")
	if !ok {
		return 0
	}
	durText, _, _ := strings.Cut(rest, ",")
	durText = strings.ReplaceAll(strings.TrimSpace(durText), " ", "")
	if durText == "" {
		return 0
	}
	d, err := time.ParseDuration(durText)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

type agyBackgroundTasks struct {
	running []proto.BackgroundTask
	ended   []proto.BackgroundTask
	due     map[string]time.Time
}

func (b agyBackgroundTasks) resolveAt(now time.Time) (running, ended []proto.BackgroundTask) {
	if len(b.due) == 0 {
		return b.running, b.ended
	}
	ended = slices.Clone(b.ended)
	expired := false
	for _, task := range b.running {
		if due, ok := b.due[task.ID]; ok && !due.IsZero() && !now.Before(due) {
			t := task
			d := due
			t.Status, t.EndedAt = proto.WorkflowCompleted, &d
			ended = append(ended, t)
			expired = true
		} else {
			running = append(running, task)
		}
	}
	if expired {
		slices.SortStableFunc(ended, func(a, b proto.BackgroundTask) int { return a.EndedAt.Compare(*b.EndedAt) })
		if extra := len(ended) - agyBackgroundEndedLimit; extra > 0 {
			ended = slices.Clone(ended[extra:])
		}
	}
	return running, ended
}

func (b agyBackgroundTasks) apply(s *proto.Session) {
	b.applyAt(s, time.Now())
}

func (b agyBackgroundTasks) applyAt(s *proto.Session, now time.Time) {
	running, ended := b.resolveAt(now)
	s.Background = nil
	if !agyStopped(s.Status) {
		s.Background = running
	}
	s.SetBackgroundEnded(recentBackgroundEnded(ended))
}

type agyTaskRecord struct {
	task          proto.BackgroundTask
	inferred      bool
	oneShot       bool
	dueAt         time.Time
	condition     string
	maxIterations int
	triggers      int
}

func (r *agyTaskRecord) finish(status string, at time.Time, inferred bool) {
	if r == nil || (r.task.Status != "" && (!r.inferred || inferred)) {
		return
	}
	r.task.Status, r.task.EndedAt, r.inferred = status, &at, inferred
}

func agyStepTime(step agyStep) time.Time {
	if updated, err := time.Parse(time.RFC3339Nano, step.UpdatedAt); err == nil {
		return updated
	}
	at, _ := time.Parse(time.RFC3339Nano, step.CreatedAt)
	return at
}

func agyBackground(steps []agyStep, conversation, dir string, brain *os.Root) agyBackgroundTasks {
	byID := make(map[string]*agyTaskRecord)
	var ordered []*agyTaskRecord
	var planner agyStep
	var lastAt time.Time
	for _, step := range steps {
		if at := agyStepTime(step); at.After(lastAt) {
			lastAt = at
		}
		switch step.Type {
		case "PLANNER_RESPONSE":
			planner = step
			for _, call := range step.ToolCalls {
				if call == nil || call.Name != "manage_task" {
					continue
				}
				var args map[string]json.RawMessage
				if json.Unmarshal(call.Args, &args) == nil && strings.EqualFold(agyToolString(args["Action"]), "kill") {
					at, _ := time.Parse(time.RFC3339Nano, step.CreatedAt)
					byID[agyToolString(args["TaskId"])].finish(proto.WorkflowKilled, at, false)
				}
			}
		case "USER_INPUT":
			planner = agyStep{}
		case "SYSTEM_MESSAGE":
			stepAt, _ := time.Parse(time.RFC3339Nano, step.CreatedAt)
			for _, m := range agyTaskEnd.FindAllStringSubmatch(step.Content, -1) {
				status := proto.WorkflowKilled
				switch strings.ToLower(m[2]) {
				case "finished":
					status = proto.WorkflowCompleted
				case "failed":
					status = proto.WorkflowFailed
				}
				byID[m[1]].finish(status, stepAt, false)
			}
			restartHandled := false
			for _, m := range agyMsgHeader.FindAllStringSubmatch(step.Content, -1) {
				hdr := m[1]
				msgAt := stepAt
				if tm := agyMsgTimestamp.FindStringSubmatch(hdr); tm != nil {
					if parsed, err := time.Parse(time.RFC3339Nano, tm[1]); err == nil {
						msgAt = parsed
					}
				}
				if agyServerRestart.MatchString(hdr) {
					restartHandled = true
					for _, rec := range ordered {
						if rec.task.Status == "" {
							rec.finish(proto.WorkflowKilled, msgAt, false)
						}
					}
				}
				sm := agyMsgSender.FindStringSubmatch(hdr)
				if sm == nil {
					continue
				}
				sender := sm[1]
				if rec := byID[sender]; rec != nil {
					if rec.oneShot {
						rec.finish(proto.WorkflowCompleted, msgAt, false)
					} else if rec.maxIterations > 0 {
						rec.triggers++
						if rec.triggers >= rec.maxIterations {
							rec.finish(proto.WorkflowCompleted, msgAt, false)
						}
					}
				}
				for _, rec := range ordered {
					if rec.task.Status != "" || !rec.oneShot || rec.condition == "" || strings.EqualFold(rec.condition, "never") || sender == rec.task.ID {
						continue
					}
					if strings.EqualFold(rec.condition, "any") || sender == rec.condition || sender == conversation+"/"+rec.condition {
						rec.finish(proto.WorkflowKilled, msgAt, false)
					}
				}
			}
			if !restartHandled && agyServerRestart.MatchString(step.Content) {
				for _, rec := range ordered {
					if rec.task.Status == "" {
						rec.finish(proto.WorkflowKilled, stepAt, false)
					}
				}
			}
		case "GENERIC":
			notice := agyBackgroundNotice(firstNonEmpty(step.backgroundStart, step.Content))
			id, details, _ := strings.Cut(notice, "\n")
			id = strings.TrimSpace(id)
			name, ours := strings.CutPrefix(id, conversation+"/")
			if !ours || !agyTaskName.MatchString(name) {
				if strings.EqualFold(step.Status, "DONE") {
					reconcileManageTaskOutput(step.Content, agyStepTime(step), byID, ordered)
				}
				continue
			}
			at, _ := time.Parse(time.RFC3339Nano, step.CreatedAt)
			record := byID[id]
			if record == nil {
				task := proto.BackgroundTask{ID: id, Kind: "shell", StartedAt: at}
				for line := range strings.SplitSeq(details, "\n") {
					line = strings.TrimSpace(line)
					if value, ok := strings.CutPrefix(line, "Task Description:"); ok && task.Description == "" {
						task.Description = strings.Clone(strings.TrimSpace(value))
					}
					if value, ok := strings.CutPrefix(line, "Task logs are available at:"); ok && task.OutputFile == "" {
						task.OutputFile = agyTaskLog(strings.TrimSpace(value), dir, conversation, name, brain)
					}
				}
				record = &agyTaskRecord{task: task}
				// Result steps have one slot per planner tool call, even when they are exported out of order.
				slot := step.seq - planner.seq - 1
				if slot >= 0 && slot < len(planner.ToolCalls) {
					if call := planner.ToolCalls[slot]; call != nil {
						setAgyTaskKind(record, call.Name, call.Args)
					}
				}
				if !record.oneShot && record.maxIterations == 0 {
					if d := agyParseTimerDesc(record.task.Description); d > 0 {
						record.task.Kind = "monitor"
						record.oneShot = true
						if !at.IsZero() {
							record.dueAt = at.Add(d)
						}
					}
				}
				byID[id] = record
				ordered = append(ordered, record)
			}
			at = agyStepTime(step)
			switch strings.ToUpper(step.Status) {
			case "DONE":
				record.finish(proto.WorkflowCompleted, at, true)
			case "ERROR":
				record.finish(proto.WorkflowFailed, at, true)
			case "CANCELED", "CANCELLED":
				record.finish(proto.WorkflowKilled, at, true)
			}
		}
	}
	var result agyBackgroundTasks
	for _, record := range ordered {
		if record.task.EndedAt == nil && !record.dueAt.IsZero() && !lastAt.Before(record.dueAt) {
			record.finish(proto.WorkflowCompleted, record.dueAt, true)
		}
		if record.task.EndedAt != nil {
			result.ended = append(result.ended, record.task)
		} else if len(result.running) < maxRuntimeBackground {
			result.running = append(result.running, record.task)
			if !record.dueAt.IsZero() {
				if result.due == nil {
					result.due = make(map[string]time.Time)
				}
				result.due[record.task.ID] = record.dueAt
			}
		}
	}
	slices.SortStableFunc(result.ended, func(a, b proto.BackgroundTask) int { return a.EndedAt.Compare(*b.EndedAt) })
	if extra := len(result.ended) - agyBackgroundEndedLimit; extra > 0 {
		result.ended = slices.Clone(result.ended[extra:])
	}
	return result
}

func reconcileManageTaskOutput(content string, at time.Time, byID map[string]*agyTaskRecord, ordered []*agyTaskRecord) {
	for _, m := range agyTaskCancelled.FindAllStringSubmatch(content, -1) {
		byID[m[1]].finish(proto.WorkflowKilled, at, false)
	}
	for _, m := range agyTaskStatus.FindAllStringSubmatch(content, -1) {
		switch strings.ToUpper(m[2]) {
		case "DONE", "COMPLETED":
			byID[m[1]].finish(proto.WorkflowCompleted, at, false)
		case "FAILED", "ERROR":
			byID[m[1]].finish(proto.WorkflowFailed, at, false)
		case "CANCELED", "CANCELLED", "KILLED", "STOPPED":
			byID[m[1]].finish(proto.WorkflowKilled, at, false)
		}
	}
	if strings.Contains(content, "No background tasks are currently running.") {
		for _, rec := range ordered {
			if rec.task.Status == "" {
				rec.finish(proto.WorkflowCompleted, at, true)
			}
		}
		return
	}
	const marker = " background task(s) currently running:\n["
	if idx := strings.Index(content, marker); idx >= 0 {
		raw := content[idx+len(marker)-1:]
		if end := strings.Index(raw, "\n"); end >= 0 {
			raw = raw[:end]
		}
		var list []struct {
			TaskID string `json:"taskId"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &list) == nil {
			live := make(map[string]bool, len(list))
			for _, entry := range list {
				live[entry.TaskID] = true
			}
			for _, rec := range ordered {
				if rec.task.Status == "" && !live[rec.task.ID] {
					rec.finish(proto.WorkflowCompleted, at, true)
				}
			}
		}
	}
}

func setAgyTaskKind(record *agyTaskRecord, tool string, raw json.RawMessage) {
	task := &record.task
	switch tool {
	case "schedule":
		task.Kind = "monitor"
		var args map[string]json.RawMessage
		if json.Unmarshal(raw, &args) == nil {
			secs := agyToolInt(args["DurationSeconds"])
			cron := strings.TrimSpace(agyToolString(args["CronExpression"]))
			cond := strings.TrimSpace(agyToolString(args["TimerCondition"]))
			maxIter := agyToolInt(args["MaxIterations"])
			if secs > 0 {
				record.oneShot = true
				record.condition = cond
				if !task.StartedAt.IsZero() {
					record.dueAt = task.StartedAt.Add(time.Duration(secs) * time.Second)
				}
			} else if cron == "" {
				if d := agyParseTimerDesc(task.Description); d > 0 {
					record.oneShot = true
					record.condition = cond
					if !task.StartedAt.IsZero() {
						record.dueAt = task.StartedAt.Add(d)
					}
				}
			} else if maxIter > 0 {
				record.maxIterations = maxIter
			}
		}
	case "run_workflow":
		task.Kind = "workflow"
	case "invoke_subagent":
		task.Kind = "agent"
	case "run_command":
		var args map[string]json.RawMessage
		if json.Unmarshal(raw, &args) == nil {
			task.Command = firstNonEmpty(agyToolString(args["CommandLine"]), agyToolString(args["command"]))
		}
	}
}

// The export can name only this task's log inside its conversation directory.
func agyTaskLog(uri, dir, conversation, name string, brain *os.Root) string {
	path, err := url.PathUnescape(agyLocalFileURLToPath(uri))
	rel := filepath.Join(".system_generated", "tasks", name+".log")
	expected, absErr := filepath.Abs(filepath.Join(dir, "brain", conversation, rel))
	// The log can be announced before it is created.
	if err != nil || absErr != nil || !filepath.IsAbs(path) || filepath.Clean(path) != expected {
		return ""
	}
	info, err := brain.Stat(rel)
	if os.IsNotExist(err) || (err == nil && info.Mode().IsRegular()) {
		return expected
	}
	return ""
}

func agyStopped(status string) bool {
	return status == proto.StatusOffline || status == proto.StatusInterrupted
}

// Runtime task metadata cannot grant access to node-local files. Join the log
// paths independently confirmed by the local conversation's transcript.
func (n *Node) agyRuntimeTaskLogs(s *proto.Session) {
	conversation, ok := strings.CutPrefix(s.SessionID, agyAgent+"-")
	if !ok || (len(s.Background) == 0 && len(s.BackgroundEnded()) == 0) {
		return
	}
	meta, ok := n.agyTranscriptMeta(conversation)
	if !ok {
		return
	}
	logs := make(map[string]string)
	for _, tasks := range [][]proto.BackgroundTask{meta.background.running, meta.background.ended} {
		for _, task := range tasks {
			if task.OutputFile != "" {
				logs[task.ID] = task.OutputFile
			}
		}
	}
	if len(logs) == 0 {
		return
	}
	attach := func(tasks []proto.BackgroundTask) []proto.BackgroundTask {
		tasks = slices.Clone(tasks)
		for i := range tasks {
			tasks[i].OutputFile = logs[tasks[i].ID]
		}
		return tasks
	}
	s.Background = attach(s.Background)
	s.SetBackgroundEnded(attach(s.BackgroundEnded()))
}
