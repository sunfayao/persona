package agy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
	"github.com/sunfayao/persona/server/internal/transcript"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

type mappedCall struct {
	name, callID string
	args         toolArgs
	item         int
}

var (
	settingsChange = lazyre.New(`(?s)<USER_SETTINGS_CHANGE>\s*(.*?)\s*</USER_SETTINGS_CHANGE>`)
	embeddedUUID   = lazyre.New(`(?i)\b[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\b`)
)

func (h *Projection) mapSteps(steps []agyStep, fallback time.Time) {
	if len(steps) == 0 {
		return
	}
	h.stepHashes = make(map[int][32]byte, len(steps))
	var turnID string
	var afterWork, afterFinish bool // afterFinish: the steps since the last planner step include a successful supervision_finish result
	var wake []int
	var calls []*mappedCall
	var plannerSeq, foreignSlots int
	tasks := make(map[string]launch)
	for _, step := range steps {
		h.seq = max(h.seq, step.seq)
		at, err := time.Parse(time.RFC3339Nano, step.CreatedAt)
		if err != nil || at.IsZero() {
			at = fallback
		}
		add := func(suffix, role, kind, text string) *proto.Item {
			h.items = append(h.items, proto.Item{ID: step.key + "#" + suffix, Seq: step.seq, Role: role, Kind: kind, TS: at, Text: text,
				Facets: &proto.ItemFacets{TurnID: turnID, Native: &proto.NativeRef{Agent: "agy", Type: step.Type}}})
			return &h.items[len(h.items)-1]
		}
		addMedia := func(role, toolUseID, clientMsgID string, attached map[string]bool) {
			var entries []json.RawMessage
			if len(step.Media) == 0 || json.Unmarshal(step.Media, &entries) != nil {
				return
			}
			for index, entry := range entries {
				var media agyMedia
				if json.Unmarshal(entry, &media) != nil || !media.isImage() || attached[agyImagePath(media.URI)] {
					continue
				}
				image := add("media"+strconv.Itoa(index), role, proto.KindImage, "")
				image.ToolUseID, image.ClientMsgID = toolUseID, clientMsgID
				image.Image = &proto.Image{Mime: media.Mime, Lazy: true}
				if role == "user" {
					image.Facets.Input = &proto.InputFacet{Via: proto.InputViaTyped}
				}
				if h.media == nil {
					h.media = make(map[string]agyMedia)
				}
				h.media[image.ID] = media
			}
		}
		h.stepHashes[step.seq] = mixHash(h.stepHashes[step.seq], step.record)
		h.usage.observeBytes(step)
		finished := afterFinish
		if step.Type != "SYSTEM_MESSAGE" && step.Type != "GENERIC" {
			afterFinish = false
		}
		switch step.Type {
		case "USER_INPUT":
			calls, wake = nil, nil
			turnID = "t" + strconv.Itoa(step.seq-1)
			h.turnError, afterWork, h.turnErrorAfterWork = "", false, false
			if step.Source != "" && step.Source != "USER_EXPLICIT" {
				continue
			}
			h.usage.turns++
			if settings := settingsChange.FindStringSubmatch(step.Content); settings != nil {
				it := add("settings", "system", proto.KindNotice, settings[1])
				model, _ := strings.CutPrefix(settings[1], "Model changed to ")
				addNotice(it, proto.NoticeCodeModelSwitched, proto.NoticeSeverityInfo, map[string]string{"model": strings.TrimSuffix(model, ".")})
			}
			text := UserText(settingsChange.ReplaceAllString(step.Content, ""))
			if text != "" {
				it := add("0", "user", proto.KindText, text)
				it.ClientMsgID = step.ClientMsgID
				it.Facets.Input = &proto.InputFacet{Via: proto.InputViaTyped}
				if h.usage.first == "" {
					h.usage.first = text
					h.firstAt, _ = time.Parse(time.RFC3339Nano, step.CreatedAt)
				}
			}
			addMedia("user", "", step.ClientMsgID, AttachedImagePaths(text))
		case "CHECKPOINT":
			it := add("0", "system", proto.KindNotice, transcript.CompactedNotice)
			it.Compact = &proto.Compact{Trigger: "auto", PreTokens: h.usage.contextTokens}
			addNotice(it, proto.NoticeCodeCompacted, proto.NoticeSeverityInfo, nil)
			if usage, _, ok := step.tokenUsage(); ok {
				it.Usage = []proto.TokenUsage{usage}
				h.usage.outputTokens = addSaturatingInt(h.usage.outputTokens, usage.Output)
			}
			h.usage.observeCheckpoint()
			h.summary, h.summarySeq = step.Content, step.seq
			foreignSlots++
		case "PLANNER_RESPONSE":
			calls, plannerSeq, foreignSlots = make([]*mappedCall, len(step.ToolCalls)), step.seq, 0
			stepModel := step.normalizedModel()
			usage, hasUsage := h.usage.observePlanner(step)
			first := len(h.items)
			if thinking := strings.TrimSpace(step.Thinking); thinking != "" {
				it := add(proto.KindThinking, "assistant", proto.KindThinking, thinking)
				it.Model = stepModel
				it.Facets.Progress = &proto.ProgressFacet{Phase: proto.ProgressPhaseThinking}
			}
			if text := strings.TrimSpace(step.Content); text != "" && text != silentEndMarker {
				it := add(proto.KindText, "assistant", proto.KindText, agyAssistantText(text))
				it.Model = stepModel
				if len(calls) != 0 {
					it.Facets.Progress = &proto.ProgressFacet{Phase: proto.ProgressPhaseCommentary}
				}
				afterWork = true
			}
			for k, call := range step.ToolCalls {
				if call == nil {
					continue
				}
				it := add("tool"+strconv.Itoa(k), "assistant", proto.KindToolUse, "")
				it.Model = stepModel
				it.ToolUseID = fmt.Sprintf("call_%s_%d", step.key, k)
				args := mapCall(call, it)
				calls[k] = &mappedCall{call.Name, it.ToolUseID, args, len(h.items) - 1}
				afterWork = true
				switch call.Name {
				case "invoke_subagent":
					h.invokesChild = true
				case "send_message":
					h.addChild(args.text("Recipient"))
				case "manage_subagents":
					var children []string
					_ = json.Unmarshal(args["ConversationIds"], &children)
					for _, id := range children {
						h.addChild(id)
					}
				case "manage_task":
					if id := args.text("TaskId"); it.ToolName == "TaskStop" && taskHandle.MatchString(id) {
						kind := proto.TaskKindShell
						if known := tasks[id].facet.Kind; known != "" {
							kind = known
						}
						it.Facets.Task = &proto.TaskFacet{ID: id, Kind: kind, Status: proto.TaskStatusCancelled}
					}
				}
			}
			if work := len(h.items) > first; work {
				if hasUsage {
					h.items[first].Usage = []proto.TokenUsage{usage}
				}
				h.turnError, h.turnErrorAfterWork = "", false
				for _, index := range wake {
					it := &h.items[index]
					it.Wake = true
					h.stepHashes[it.Seq] = mixHash(h.stepHashes[it.Seq], step.record)
				}
				wake = nil
			}
		case "GENERIC":
			slot := step.seq - plannerSeq - 1 - foreignSlots
			var call *mappedCall
			if slot >= 0 && slot < len(calls) {
				call = calls[slot]
			}
			it := add("0", "user", proto.KindToolResult, "")
			it.ToolUseID = "call_" + step.key + "_0"
			mapResult(step, it)
			if call != nil {
				h.mapCallResult(it, call)
				afterFinish = afterFinish || !it.IsError && strings.HasSuffix(h.items[call.item].ToolName, "supervision_finish")
				if call.name == "schedule" && it.IsError {
					use := &h.items[call.item]
					h.stepHashes[use.Seq] = mixHash(h.stepHashes[use.Seq], step.record)
				}
			}
			if t := backgroundLaunch(h, step, call); t != nil {
				if call == nil || call.name != "schedule" {
					switch {
					case it.IsError:
						t.Status = proto.TaskStatusFailed
					case step.Status == "DONE":
						t.Status = proto.TaskStatusCompleted
					case step.Status == "CANCELED" || step.Status == "CANCELLED":
						t.Status = proto.TaskStatusCancelled
					}
					t.ExitCode = it.Outcome.ExitCode
					it.Facets.Task = t
					h.recordLaunch(tasks, t, call)
					if call != nil {
						use := &h.items[call.item]
						use.Facets.Tool.TaskID = t.ID
						h.stepHashes[use.Seq] = mixHash(h.stepHashes[use.Seq], step.record)
						it.Facets.Tool.TaskID = t.ID
					}
				}
			}
			if call != nil && call.name == "schedule" && !it.IsError {
				t := scheduledTask(call, it, step, backgroundLaunch(h, step, call))
				use := &h.items[call.item]
				use.Facets.Task, use.Facets.Tool.TaskID = t, t.ID
				h.stepHashes[use.Seq] = mixHash(h.stepHashes[use.Seq], step.record)
				it.Facets.Tool.TaskID = t.ID
				h.recordLaunch(tasks, t, call)
			}
			h.addSpill(it, step.seq-1)
			addMedia("assistant", it.ToolUseID, "", nil)
			afterWork = true
		case "SYSTEM_MESSAGE":
			if len(calls) > 0 {
				foreignSlots++
			}
			for i, event := range systemEvents(step.Content, at, tasks) {
				it := add(strconv.Itoa(i), "system", proto.KindNotice, event.text)
				it.Task, it.Facets.Task, it.Facets.ParentID = event.note, event.task, event.parent
				addNotice(it, event.code, event.severity, event.args)
				if event.canWake {
					wake = append(wake, len(h.items)-1)
				}
			}
		case "PERSONA_MODE_SWITCH":
			it := add("0", "system", proto.KindNotice, step.Content)
			addNotice(it, proto.NoticeCodeModeSwitched, proto.NoticeSeverityInfo, nil)
		case "ERROR_MESSAGE", "PERSONA_ERROR":
			if step.Type == "ERROR_MESSAGE" {
				calls = nil
			}
			if isAgyInterruptedStep(step) || step.Type == "ERROR_MESSAGE" && finished && emptyOutputError(step) {
				continue
			}
			text := strings.TrimSpace(textfmt.FirstNonEmpty(step.Error, step.Content))
			h.turnError, h.turnErrorAfterWork = text, afterWork
			if text == "" {
				continue
			}
			it := add("0", "system", proto.KindNotice, text)
			addNotice(it, proto.NoticeCodeAPIError, proto.NoticeSeverityError, map[string]string{"error": strings.TrimSpace(strings.TrimPrefix(text, "API Error:"))})
		}
		// An unanswered schedule can still be shown using its call ID. A later result replaces that provisional identity.
		if step.Type == "PLANNER_RESPONSE" {
			for _, call := range calls {
				if call != nil && call.name == "schedule" {
					h.items[call.item].Facets.Task = scheduledTask(call, nil, agyStep{}, nil)
				}
			}
		}
	}
	h.row = turnmodel.Reduce(h.items)
}

func (h *Projection) mapCallResult(it *proto.Item, call *mappedCall) {
	use := &h.items[call.item]
	it.ToolUseID, it.Facets.ParentID, it.Facets.Native.Name = call.callID, call.callID, call.name
	t := *use.Facets.Tool
	switch it.Outcome.Status {
	case proto.OutcomeOK:
		t.Status = proto.ToolStatusCompleted
	case proto.OutcomeFailed:
		t.Status = proto.ToolStatusFailed
	case proto.OutcomeBackground:
		t.Status = proto.ToolStatusBackground
	}
	it.Facets.Tool = &t
	if call.name == "schedule" && it.IsError {
		use.Facets.Task = nil
	}
	if q := use.Facets.Interaction; q != nil && !it.IsError {
		if a := answers(it.Text, q.Questions); len(a) > 0 {
			it.Outcome.Answers = a
			answered := *q
			answered.Status, answered.Answers = proto.InteractionStatusAnswered, a
			it.Facets.Interaction = &answered
		}
	}
	if call.name == "invoke_subagent" {
		for _, id := range embeddedUUID.FindAllString(it.Text, -1) {
			h.addChild(id)
			if it.Facets.Subagent == nil {
				it.Facets.Subagent = &proto.SubagentFacet{ID: id, Relation: proto.SubagentRelationSpawn}
			}
		}
	}
	if !it.IsError && (use.ToolName == "Edit" || use.ToolName == "Write") {
		if path := call.args.text("TargetFile", "Path", "file_path"); path != "" {
			it.Outcome.Files = []proto.OutcomeFile{{Path: path}}
		}
	}
}

func (h *Projection) recordLaunch(tasks map[string]launch, t *proto.TaskFacet, call *mappedCall) {
	l := launch{facet: *t}
	if call != nil {
		l.call = call.callID
	}
	tasks[t.ID] = l
}

func (h *Projection) addChild(id string) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !conversationID.MatchString(id) {
		return
	}
	h.invokesChild = true
	if h.children == nil {
		h.children = make(map[string]bool)
	}
	h.children[id] = true
}

func mixHash(a, b [32]byte) [32]byte {
	var both [64]byte
	copy(both[:32], a[:])
	copy(both[32:], b[:])
	return sha256.Sum256(both[:])
}

func AttachedImagePaths(text string) map[string]bool {
	var paths map[string]bool
	for {
		_, rest, open := strings.Cut(text, "<attached_images>")
		block, tail, closed := strings.Cut(rest, "</attached_images>")
		if !open || !closed {
			return paths
		}
		for line := range strings.SplitSeq(block, "\n") {
			if path := agyImagePath(strings.TrimSpace(line)); path != "" {
				if paths == nil {
					paths = make(map[string]bool)
				}
				paths[path] = true
			}
		}
		text = tail
	}
}

// silentEndMarker is what a supervision turn ends with on this runtime, which must say something after a tool
// result (assistant/supervision_instructions). It is no reply: it is not shown.
const silentEndMarker = "[silent]"

// emptyOutputError: the runtime's complaint that the model said nothing after a tool result. After a successful
// supervision_finish that is the intended end of the turn, not an API error.
func emptyOutputError(step agyStep) bool {
	return strings.Contains(textfmt.FirstNonEmpty(step.Error, step.Content), "model output must contain either output text or tool calls")
}
