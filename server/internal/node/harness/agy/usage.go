package agy

import (
	"bytes"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// ContextWindow is the native compaction threshold measured in the full export.
const ContextWindow = 512_000

type agyTranscriptUsage struct {
	model, first                string
	modelAt                     time.Time
	contextTokens, outputTokens int
	compactions, turns          int
	hasUsage                    bool
	contextStats                transcript.ContextStats
}

func firstPositive(vals ...int64) int64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func addSaturatingInt(a int, b int64) int {
	if b <= 0 {
		return a
	}
	if b > math.MaxInt-int64(a) {
		return math.MaxInt
	}
	return a + int(b)
}

func (s agyStep) normalizedModel() string {
	m := strings.TrimSpace(s.Model)
	if strings.HasPrefix(m, "<") && strings.HasSuffix(m, ">") {
		return ""
	}
	return m
}

func (s agyStep) tokenUsage() (usage proto.TokenUsage, contextTokens int, ok bool) {
	var u agyStepUsage
	if s.Usage != nil {
		u = *s.Usage
	}
	in := firstPositive(s.InputTokens, u.InputTokens)
	out := firstPositive(s.OutputTokens, u.OutputTokens)
	cacheRead := firstPositive(s.CacheReadTokens, s.CacheReadInputTokens, u.CacheReadTokens, u.CacheReadInputTokens)
	cacheWrite := firstPositive(s.CacheWriteTokens, s.CacheCreationInputTokens, u.CacheWriteTokens, u.CacheCreationInputTokens)
	cacheWrite1h := firstPositive(s.CacheWrite1hTokens, u.CacheWrite1hTokens)
	cacheWrite = max(cacheWrite, cacheWrite1h)
	if in == 0 && out == 0 && cacheRead == 0 && cacheWrite == 0 {
		return proto.TokenUsage{}, 0, false
	}
	return proto.TokenUsage{Model: s.normalizedModel(), Input: in, Output: out, CacheRead: cacheRead, CacheWrite: cacheWrite, CacheWrite1h: cacheWrite1h},
		addSaturatingInt(addSaturatingInt(0, in), cacheRead), true
}

func (u *agyTranscriptUsage) observeCheckpoint() {
	u.compactions++
	u.contextTokens = 0
	u.contextStats.ResetCompacted()
}

func (u *agyTranscriptUsage) observePlanner(step agyStep) (proto.TokenUsage, bool) {
	if model := step.normalizedModel(); model != "" {
		u.model = model
		if at, err := time.Parse(time.RFC3339Nano, step.CreatedAt); err == nil {
			u.modelAt = at
		}
	}
	usage, tokens, ok := step.tokenUsage()
	if ok {
		u.hasUsage = true
		if tokens > 0 {
			u.contextTokens = tokens
		}
		u.outputTokens = addSaturatingInt(u.outputTokens, usage.Output)
	}
	return usage, ok
}

func (u *agyTranscriptUsage) observeBytes(step agyStep) {
	s := &u.contextStats
	weigh := transcript.EstimateWeightedBytes
	switch step.Type {
	case "USER_INPUT":
		w := weigh(step.Content)
		if step.Source == "SYSTEM" || strings.Contains(step.Content, "<system-reminder>") || strings.Contains(step.Content, "<persona-context") {
			s.SystemBytes += w
		} else {
			s.ConversationBytes += w
		}
		if len(bytes.TrimSpace(step.Media)) > 2 {
			s.ConversationBytes += 6400
		}
	case "SYSTEM_MESSAGE":
		s.SystemBytes += weigh(step.Content)
	case "PLANNER_RESPONSE":
		s.ConversationBytes += weigh(step.Content) + weigh(step.Thinking)
		for _, tc := range step.ToolCalls {
			if tc != nil {
				s.ToolOutputBytes += int64(len(tc.Name)) + weigh(string(tc.Args))
			}
		}
	case "GENERIC", "ERROR_MESSAGE":
		s.ToolOutputBytes += weigh(step.Content) + weigh(step.Error)
	}
}

func (h *Projection) Model() (string, time.Time) { return h.usage.model, h.usage.modelAt }

// Apply fills the transcript-owned state, without overwriting the runtime's controls or turn error.
func (h *Projection) Apply(s *proto.Session, pane bool) {
	u := &h.usage
	s.ContextTokens, s.ContextWindow = proto.Tokens32(u.contextTokens), ContextWindow
	s.OutputTokens, s.Rev = u.outputTokens, h.rev
	h.row.Apply(s)
	ended := h.row.BackgroundEnded
	for i, task := range h.row.BackgroundEnded {
		if task.Status == proto.TaskStatusCancelled {
			if &ended[0] == &h.row.BackgroundEnded[0] {
				ended = slices.Clone(ended)
			}
			ended[i].Status = proto.WorkflowKilled // The legacy background row uses the older status word.
		}
	}
	s.SetBackgroundEnded(ended)
	if s.Facets == nil {
		s.Facets = &proto.SessionFacets{}
	}
	s.Facets.Context = &proto.ContextMeter{
		Used: int64(u.contextTokens), AutoCompactAt: ContextWindow, Source: proto.ContextSourceRule,
	}
	s.SetContextBreakdown(transcript.BuildContextBreakdown(u.contextStats, u.contextTokens, ContextWindow, ContextWindow, true))
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = h.ModTime().UTC()
	}
	if pane {
		if s.Model == "" {
			s.Model = u.model
		}
		s.TurnError, s.TurnErrorAfterWork = h.turnError, h.turnErrorAfterWork
		s.TurnErrorCode = proto.UnrecoverableTurnErrorCode(s.TurnError)
	}
}

func (h *Projection) FillInfo(info map[string]any) {
	var s proto.Session
	h.Apply(&s, false)
	info["context_tokens"], info["context_window"], info["output_tokens"] = h.usage.contextTokens, ContextWindow, h.usage.outputTokens
	info["compactions"], info["turns"] = h.usage.compactions, h.usage.turns
	if h.usage.hasUsage {
		delete(info, "usage_unknown")
	} else {
		info["usage_unknown"] = true
	}
	info["background"], info["background_ended"], info["plan"], info["loop"], info["facets"] = s.Background, s.BackgroundEnded(), s.Plan, s.Loop, s.Facets
	if bd := s.ContextBreakdown(); bd != nil {
		info["context_breakdown"] = bd
	}
}

func (h *Projection) TaskCommand(id string) string {
	for _, task := range h.row.Background {
		if task.ID == id {
			return textOfTask(task)
		}
	}
	for _, task := range h.row.BackgroundEnded {
		if task.ID == id {
			return textOfTask(task)
		}
	}
	return ""
}

func textOfTask(t proto.BackgroundTask) string {
	if t.Description != "" {
		return t.Description
	}
	return t.Command
}
