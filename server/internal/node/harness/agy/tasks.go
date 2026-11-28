package agy

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
	"github.com/sunfayao/persona/server/internal/transcript"
)

const taskPrefix = "Tool is running as a background task with task id:"

var (
	taskHandle    = lazyre.New(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}/task-[0-9]+$`)
	taskEnd       = lazyre.New(`(?i)^Task id "([^"\n]+)" (?:was )?(finished|failed|canceled|cancelled|killed|stopped)\b`)
	messageHeader = lazyre.New(`(?m)^\[Message\][^\n]*?\bsender=(\S+) [^\n]*?\bcontent=`)
	taskStillRuns = lazyre.New(`(?i)^background command is still running after [0-9]+s\b`)
	serverRestart = "All your subagents and background tasks have been stopped due to server restart"
)

func agyBackgroundNotice(text string) string {
	body, _ := resultText(text)
	rest, _ := strings.CutPrefix(strings.TrimSpace(body), taskPrefix)
	return strings.TrimSpace(rest)
}

type launch struct {
	facet proto.TaskFacet
	call  string
	ended bool // its end or a restart notice was seen
}

func backgroundLaunch(h *Projection, s agyStep, call *mappedCall) *proto.TaskFacet {
	body := agyBackgroundNotice(s.backgroundStart)
	if body == "" {
		body = agyBackgroundNotice(s.Content)
	}
	id, details, _ := strings.Cut(body, "\n")
	id = strings.TrimSpace(id)
	if !taskHandle.MatchString(id) {
		return nil
	}
	t := &proto.TaskFacet{ID: id, Kind: proto.TaskKindShell, Status: proto.TaskStatusRunning}
	_, afterTitle, hasTitle := strings.Cut(details, "Task Description:")
	if hasTitle {
		title, _, _ := strings.Cut(afterTitle, "\nTask logs are available at:")
		t.Title = strings.TrimSpace(title)
	}
	if _, tail, ok := strings.Cut(details, "Task logs are available at:"); ok {
		uri, _, _ := strings.Cut(tail, "\n")
		// Exported paths do not authorize arbitrary file reads; only the native task's own log is attachable.
		name := filepath.Base(id)
		expected, err := filepath.Abs(filepath.Join(h.source.Root, "brain", h.source.Conversation, ".system_generated", "tasks", name+".log"))
		if err == nil && id == h.source.Conversation+"/"+name && agyImagePath(strings.TrimSpace(uri)) == expected {
			brain, err := h.source.open()
			if err == nil {
				defer brain.Close()
				entry, err := brain.Lstat(filepath.Join(".system_generated", "tasks", name+".log"))
				if os.IsNotExist(err) || err == nil && entry.Mode().IsRegular() {
					t.OutputFile = expected
				}
			}
		}
	}
	if call != nil {
		t.Command = call.args.text("CommandLine", "command")
	}
	return t
}

func scheduledTask(call *mappedCall, result *proto.Item, s agyStep, task *proto.TaskFacet) *proto.TaskFacet {
	t := &proto.TaskFacet{ID: call.callID, Kind: proto.TaskKindTimer, Status: proto.TaskStatusScheduled}
	if task != nil {
		t.ID = task.ID
	}
	a := call.args
	delay, cron := a.count("DurationSeconds"), a.text("CronExpression")
	t.Timer = &proto.TimerSpec{IntervalS: delay, Cron: cron, Condition: a.text("TimerCondition"), MaxRuns: a.count("MaxIterations"), Prompt: a.text("Prompt")}
	t.Title = t.Timer.Prompt
	if delay > 0 {
		if cron == "" && t.Timer.MaxRuns == 0 {
			t.Timer.MaxRuns = 1 // Native delays fire once unless the call explicitly asks for iterations.
		}
		if result != nil {
			if at, err := time.Parse(time.RFC3339Nano, s.CreatedAt); err == nil && int64(delay) <= int64DurationSeconds {
				t.Timer.NextAt = at.Add(time.Duration(delay) * time.Second)
			}
		}
	}
	return t
}

const int64DurationSeconds = (1<<63 - 1) / int64(time.Second)

func addNotice(it *proto.Item, code, severity string, args map[string]string) {
	it.Facets.Notice = &proto.NoticeFacet{Code: code, Severity: severity, Args: args}
}

type systemEvent struct {
	code, text, severity string
	args                 map[string]string
	parent               string
	note                 *proto.TaskNote
	task                 *proto.TaskFacet
	canWake              bool
}

// stoppedByRestart ends every launch in tasks and says whether one of them, or a subagent invoked since the last
// restart notice, was still going: the runtime's restart notice is routine when none was.
func stoppedByRestart(tasks map[string]launch, subagents *bool) bool {
	stopped := *subagents
	*subagents = false
	for id, l := range tasks {
		if !l.ended && (l.facet.Status == proto.TaskStatusRunning || l.facet.Status == proto.TaskStatusScheduled) {
			stopped = true
		}
		l.ended = true
		tasks[id] = l
	}
	return stopped
}

// systemEvents maps the runtime's system messages. The restart notice ("All your subagents and background tasks
// have been stopped due to server restart") is the closed-source runtime's own text, kept in its conversation where
// the model reads it: here it is a warning only when something was running, else an info notice.
func systemEvents(content string, at time.Time, tasks map[string]launch, subagents *bool) []systemEvent {
	matches := messageHeader.FindAllStringSubmatchIndex(content, -1)
	var out []systemEvent
	for i, m := range matches {
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content[m[1]:end]), "</SYSTEM_MESSAGE>"))
		sender := content[m[2]:m[3]]
		e := systemEvent{text: transcript.BackgroundTaskUpdate, severity: proto.NoticeSeverityInfo}
		switch {
		case sender == "system" && strings.Contains(body, serverRestart):
			e.code = proto.NoticeCodeServerRestart
			if stoppedByRestart(tasks, subagents) {
				e.severity = proto.NoticeSeverityWarning
			}
		case taskStillRuns.MatchString(body):
			continue
		default:
			if end := taskEnd.FindStringSubmatch(body); end != nil && taskHandle.MatchString(end[1]) {
				known := tasks[end[1]]
				known.ended = true
				tasks[end[1]] = known
				status := proto.TaskStatusCancelled
				legacy := "killed"
				code := commandExit(body)
				switch strings.ToLower(end[2]) {
				case "finished":
					status, legacy = proto.TaskStatusCompleted, "completed"
					if code != nil && *code != 0 {
						status, legacy = proto.TaskStatusFailed, "failed"
					}
				case "failed":
					status, legacy = proto.TaskStatusFailed, "failed"
				}
				if status == proto.TaskStatusFailed {
					e.severity = proto.NoticeSeverityError
				}
				kind := proto.TaskKindShell
				if known.facet.Kind != "" {
					kind = known.facet.Kind
				}
				e.code, e.canWake, e.parent = proto.NoticeCodeTaskUpdate, true, known.call
				e.task = &proto.TaskFacet{ID: end[1], Kind: kind, Status: status, ExitCode: code, Title: known.facet.Title}
				detail := strings.TrimSpace(strings.TrimPrefix(body[len(end[0]):], " with result:"))
				e.note = &proto.TaskNote{Kind: proto.TaskKindShell, Status: legacy, TaskID: end[1], ToolUseID: known.call,
					Title: known.facet.Title, ExitCode: code, Detail: textfmt.Clip(detail, 2048)}
			} else if known, ok := tasks[sender]; ok && known.facet.Kind == proto.TaskKindTimer {
				e.code, e.canWake, e.parent = proto.NoticeCodeTimerFired, true, known.call
				prompt := known.facet.Title
				if prompt == "" {
					prompt = body
				}
				t := known.facet
				t.Status = proto.TaskStatusFired
				if spec := t.Timer; spec != nil && spec.IntervalS > 0 && int64(spec.IntervalS) <= int64DurationSeconds {
					t.Timer = new(*spec)
					t.Timer.NextAt = at.Add(time.Duration(spec.IntervalS) * time.Second)
				}
				e.task = &t
				e.args = map[string]string{"prompt": prompt}
				e.note = &proto.TaskNote{Kind: "monitor", Status: "event", Title: prompt, TaskID: sender, ToolUseID: known.call}
			} else {
				continue // Includes efficiency reminders, which are model-only context.
			}
		}
		out = append(out, e)
	}
	return out
}
