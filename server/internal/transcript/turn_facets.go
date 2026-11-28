package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

func itemFacets(item *proto.Item) *proto.ItemFacets {
	if item.Facets == nil {
		item.Facets = &proto.ItemFacets{}
	}
	return item.Facets
}

func (m *Meta) projectItemFacets(item *proto.Item, plan *proto.PlanFacet) {
	f := itemFacets(item)
	agent := m.facetAgent
	if agent == "auto" {
		agent = "claude"
		if m.Codex {
			agent = "codex"
		}
	}
	f.Native = &proto.NativeRef{Agent: agent, Type: m.facetNative, Name: item.ToolName}
	if m.Codex && m.codexTurnID != "" {
		m.facetTurn = m.codexTurnID
	}
	if item.TurnDuration != nil && item.TurnDuration.TurnID != "" {
		m.facetTurn = item.TurnDuration.TurnID
	}
	if item.Role == "user" && (item.Kind == proto.KindText || item.Kind == proto.KindImage) && !m.localInput {
		if !item.Queued {
			m.redactedThinkingMS = 0
		}
		via := proto.InputViaTyped
		switch {
		case item.Queued:
			via = proto.InputViaQueued
		case item.Source != nil && item.Source.Kind == proto.SourceScheduledTask:
			via = proto.InputViaScheduled
		case item.From != "":
			via = proto.InputViaRelay
		}
		f.Input = &proto.InputFacet{Via: via}
		if !m.Codex && !item.Queued && (item.Kind == proto.KindText || m.facetTurn == "") {
			m.facetTurn = fmt.Sprintf("t%d", item.Seq)
		}
	}
	f.TurnID = m.facetTurn

	switch item.Kind {
	case proto.KindThinking:
		if f.Progress == nil {
			f.Progress = &proto.ProgressFacet{Phase: proto.ProgressPhaseThinking, Redacted: item.Text == ""}
		}
	case proto.KindText:
		if item.Narration {
			f.Progress = &proto.ProgressFacet{Phase: proto.ProgressPhaseCommentary}
		}
	case proto.KindToolUse:
		tool := describeTool(item)
		f.Tool = &tool
		if item.ToolUseID != "" {
			if m.facetTools == nil || len(m.facetTools) >= 1024 {
				m.facetTools = make(map[string]proto.ToolFacet)
				m.facetArtifacts = nil
			}
			m.facetTools[item.ToolUseID] = tool
		}
		f.Interaction = toolInteraction(item, tool)
		if tool.Kind == proto.ToolKindArtifact {
			f.Artifact = toolArtifact(item)
			if f.Artifact != nil && item.ToolUseID != "" {
				if m.facetArtifacts == nil {
					m.facetArtifacts = make(map[string]*proto.ArtifactFacet)
				}
				m.facetArtifacts[item.ToolUseID] = f.Artifact
			}
		}
	case proto.KindToolResult:
		if tool, ok := m.facetTools[item.ToolUseID]; ok {
			tool.Status = outcomeStatus(item)
			f.Tool = &tool
			f.ParentID = item.ToolUseID
			if tool.Status != proto.ToolStatusBackground {
				delete(m.facetTools, item.ToolUseID)
				if tool.Status == proto.ToolStatusCompleted {
					f.Artifact = m.facetArtifacts[item.ToolUseID]
				}
				delete(m.facetArtifacts, item.ToolUseID)
			}
			if tool.Kind == proto.ToolKindAsk && item.Outcome != nil && len(item.Outcome.Answers) != 0 {
				f.Interaction = &proto.InteractionFacet{ID: item.ToolUseID, Kind: proto.InteractionKindQuestion,
					Status: proto.InteractionStatusAnswered, Answers: item.Outcome.Answers}
			}
			if tool.Kind == proto.ToolKindSchedule && !item.IsError {
				f.Task = loopFacet(m.Loop, tool.Name)
			}
		}
	case proto.KindNotice:
		if f.Notice == nil {
			f.Notice = transcriptNotice(item, m.facetNative)
		}
	case proto.KindVisualization:
		if v := item.Visualization; v != nil {
			f.Artifact = &proto.ArtifactFacet{ID: item.ID, Kind: proto.ArtifactKindVisualization,
				Op: proto.ArtifactOpCreated, Path: v.Path, Title: v.Title, Mode: v.Mode}
		}
	}
	if item.Role == "assistant" && m.redactedThinkingMS > 0 {
		if f.Progress == nil {
			f.Progress = &proto.ProgressFacet{Phase: proto.ProgressPhaseThinking, Redacted: true, DurationMs: m.redactedThinkingMS}
		}
		m.redactedThinkingMS = 0
	}
	f.Plan = plan
	if task := item.Task; task != nil && task.TaskID != "" {
		f.Task = taskFacet(task)
		if task.Kind == proto.TaskKindAgent {
			f.Subagent = subagentFacet(task)
		}
	}
}

func (m *Meta) projectBackgroundResult(item *proto.Item, evidence ToolTaskEvidence) {
	id := evidence.ShellID
	if id == "" {
		id = evidence.MonitorID
	}
	if id == "" {
		return
	}
	for _, task := range m.Background {
		if task.ID == id {
			itemFacets(item).Task = &proto.TaskFacet{ID: id, Kind: task.Kind, Status: proto.TaskStatusRunning,
				Command: task.Command, Title: task.Description, OutputFile: task.OutputFile}
			return
		}
	}
}

func loopFacet(loop *proto.LoopInfo, tool string) *proto.TaskFacet {
	if tool == "CronDelete" {
		return &proto.TaskFacet{ID: "/loop", Kind: proto.TaskKindTimer, Status: proto.TaskStatusCancelled}
	}
	if loop == nil {
		return nil
	}
	return &proto.TaskFacet{ID: "/loop", Kind: proto.TaskKindTimer, Status: proto.TaskStatusScheduled, Title: loop.Reason,
		Timer: &proto.TimerSpec{NextAt: loop.NextAt, IntervalS: loop.IntervalSeconds, Cron: loop.Cron, Prompt: loop.Prompt}}
}

func describeTool(item *proto.Item) proto.ToolFacet {
	name := turnmodel.NameOf(item.ToolName)
	tool := proto.ToolFacet{Name: name, Kind: turnmodel.KindOf(name), Status: proto.ToolStatusRunning}
	if server, toolName, ok := turnmodel.SplitMCP(name); ok {
		tool.Mcp = &proto.McpRef{Server: server, Tool: toolName}
	}
	var input struct {
		FilePath    string `json:"file_path"`
		Description string `json:"description"`
		Offset      int    `json:"offset"`
		Limit       int    `json:"limit"`
		TaskID      string `json:"task_id"`
	}
	_ = json.Unmarshal(item.ToolInput, &input)
	tool.Title, tool.TaskID = oneLine(input.Description, 120), input.TaskID
	if input.FilePath != "" {
		end := 0
		if input.Offset > 0 && input.Limit > 0 {
			end = input.Offset + input.Limit - 1
		}
		tool.Locations = []proto.ToolLocation{{Path: input.FilePath, Line: input.Offset, EndLine: end}}
	}
	if tool.Kind == proto.ToolKindExecute {
		tool.Language = proto.ToolLanguageShell
	}
	return tool
}

func toolArtifact(item *proto.Item) *proto.ArtifactFacet {
	var in struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Title  string `json:"title"`
		Action string `json:"action"`
		Kind   string `json:"kind"`
	}
	if json.Unmarshal(item.ToolInput, &in) != nil || (in.ID == "" && in.Path == "") {
		return nil
	}
	if in.ID == "" {
		in.ID = in.Path
	}
	op := proto.ArtifactOpCreated
	switch in.Action {
	case "", "create":
	case "update", "replace":
		op = proto.ArtifactOpUpdated
	case "delete":
		op = proto.ArtifactOpDeleted
	default:
		return nil
	}
	if in.Kind == "" {
		in.Kind = proto.ArtifactKindFile
	}
	return &proto.ArtifactFacet{ID: in.ID, Path: in.Path, Title: in.Title, Op: op, Kind: in.Kind}
}

func toolInteraction(item *proto.Item, tool proto.ToolFacet) *proto.InteractionFacet {
	var record proto.InteractionFacet
	record.ID, record.Status = item.ToolUseID, proto.InteractionStatusPending
	switch tool.Kind {
	case proto.ToolKindAsk:
		record.Kind = proto.InteractionKindQuestion
		var input struct {
			Questions []struct {
				Question    string                    `json:"question"`
				Header      string                    `json:"header"`
				Options     []proto.InteractionOption `json:"options"`
				MultiSelect bool                      `json:"multiSelect"`
			} `json:"questions"`
		}
		if json.Unmarshal(item.ToolInput, &input) != nil {
			return nil
		}
		for _, q := range input.Questions {
			record.Questions = append(record.Questions, proto.InteractionQuestion{
				Question: q.Question, Header: q.Header, Options: q.Options, MultiSelect: q.MultiSelect,
			})
		}
	case proto.ToolKindMode:
		if tool.Name != "ExitPlanMode" {
			return nil
		}
		record.Kind = proto.InteractionKindPlan
		_ = json.Unmarshal(item.ToolInput, &record)
	default:
		return nil
	}
	return &record
}

func outcomeStatus(item *proto.Item) string {
	if o := item.Outcome; o != nil {
		switch o.Status {
		case proto.OutcomeFailed, proto.OutcomeTimedOut:
			return proto.ToolStatusFailed
		case proto.OutcomeDenied, proto.OutcomeBlocked:
			return proto.ToolStatusDenied
		case proto.OutcomeInterrupted:
			return proto.ToolStatusInterrupted
		case proto.OutcomeBackground:
			return proto.ToolStatusBackground
		}
	}
	if item.IsError {
		return proto.ToolStatusFailed
	}
	return proto.ToolStatusCompleted
}

func taskFacet(note *proto.TaskNote) *proto.TaskFacet {
	switch note.Kind {
	case proto.TaskKindShell, proto.TaskKindMonitor, proto.TaskKindAgent, proto.TaskKindWorkflow, proto.TaskKindTimer:
	default:
		return nil
	}
	status := note.Status
	switch status {
	case "stopped", "killed":
		status = proto.TaskStatusCancelled
	case "ended":
		status = proto.TaskStatusCompleted
	}
	return &proto.TaskFacet{ID: note.TaskID, Kind: note.Kind, Status: status, Title: note.Title, ExitCode: note.ExitCode, OutputFile: note.OutputFile}
}

func subagentFacet(note *proto.TaskNote) *proto.SubagentFacet {
	relation, status := proto.SubagentRelationResult, note.Status
	switch status {
	case "running":
		relation = proto.SubagentRelationSpawn
	case "stopped", "killed":
		relation, status = proto.SubagentRelationCancel, proto.SubagentStatusCancelled
	case "ended":
		status = proto.SubagentStatusCompleted
	}
	return &proto.SubagentFacet{ID: note.TaskID, Name: note.Title, Relation: relation, Status: status}
}

func transcriptNotice(item *proto.Item, native string) *proto.NoticeFacet {
	notice := &proto.NoticeFacet{Severity: proto.NoticeSeverityInfo}
	switch {
	case item.Task != nil:
		notice.Code = proto.NoticeCodeTaskUpdate
	case item.Compact != nil || item.Text == CompactedNotice:
		notice.Code = proto.NoticeCodeCompacted
	case item.Text == InterruptedNotice:
		notice.Code = proto.NoticeCodeInterrupted
	case item.ModelRefusal != nil:
		notice.Code = proto.NoticeCodeModelFallback
	case native == "system/api_error" || strings.HasPrefix(item.Text, APIErrorPrefix):
		notice.Code, notice.Severity = proto.NoticeCodeAPIError, proto.NoticeSeverityError
		notice.Args = map[string]string{"error": strings.TrimSpace(strings.TrimPrefix(item.Text, APIErrorPrefix))}
		if strings.HasSuffix(item.Text, APIRetrying) {
			notice.Code, notice.Severity = proto.NoticeCodeRetrying, proto.NoticeSeverityWarning
		}
	case native == "system/scheduled_task_fire":
		notice.Code = proto.NoticeCodeLoopFired
	case item.IsError:
		notice.Code, notice.Severity = proto.NoticeCodeRuntimeError, proto.NoticeSeverityError
		notice.Args = map[string]string{"error": item.Text}
	default:
		return nil
	}
	return notice
}
