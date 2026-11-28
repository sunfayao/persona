package agy

import (
	"bytes"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// ContextWindow is the default 1M context window for Antigravity models when neither the
// transcript nor the runtime reports a model-specific window.
const (
	ContextWindow       = 1_000_000
	gptOSSContextWindow = 131_072
)

// ContextPolicy overrides how ApplyWithPolicy and FillInfoWithPolicy resolve the model's
// context window and auto-compaction threshold for an Antigravity session.
type ContextPolicy struct {
	Window        int  // explicit model window (from ACP usage_update or managed launcher), 0 to infer
	WindowNative  bool // true when Window came directly from runtime telemetry (ACP usage_update)
	AutoCompactAt int  // explicit auto-compaction threshold (from main_compact_window, env, or settings), 0 to follow Window
}

type agyTranscriptUsage struct {
	model, first                string
	modelAt                     time.Time
	contextTokens, outputTokens int
	contextWindow               int
	compactions, turns          int
	hasUsage                    bool
	contextStats                transcript.ContextStats
}

// WindowForModel returns the context window for an Antigravity model when recorded > 0 is not provided.
func WindowForModel(model string, recorded int) int {
	if recorded > 0 {
		return recorded
	}
	id := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(id, "gpt-oss-120b") {
		return gptOSSContextWindow
	}
	return ContextWindow
}

// ParseAutoCompactOverride parses agy's AGY_CLI_AUTO_COMPACT_OVERRIDE format:
// exact tokens ("1000000"), K/M shorthand ("900K", "1M", "0.5M"), or percentage ("80%").
// When maxWindow > 0, values above maxWindow are ignored (matching agy's store.AutoCompactOverride).
func ParseAutoCompactOverride(raw string, maxWindow int) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "%") {
		if maxWindow <= 0 {
			return 0
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "%")), 64)
		if err != nil || math.IsNaN(pct) || math.IsInf(pct, 0) || pct <= 0 || pct > 100 {
			return 0
		}
		return int(math.Floor(float64(maxWindow) * pct / 100))
	}
	upper := strings.ToUpper(s)
	mult := 1.0
	switch {
	case strings.HasSuffix(upper, "K"):
		upper = strings.TrimSpace(strings.TrimSuffix(upper, "K"))
		mult = 1_000
	case strings.HasSuffix(upper, "M"):
		upper = strings.TrimSpace(strings.TrimSuffix(upper, "M"))
		mult = 1_000_000
	}
	val, err := strconv.ParseFloat(upper, 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return 0
	}
	tokens := int(math.Floor(val * mult))
	if tokens <= 0 || (maxWindow > 0 && tokens > maxWindow) {
		return 0
	}
	return tokens
}

// ParseContextWindowPreset parses agy settings.json's contextWindow preset ("256K", "512K", "900K", "1M", or tokens).
func ParseContextWindowPreset(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasSuffix(s, "%") {
		return 0
	}
	return ParseAutoCompactOverride(s, 0)
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
	var nestedWindow int64
	if step.Usage != nil {
		nestedWindow = step.Usage.ModelContextWindow
	}
	if w := firstPositive(step.ModelContextWindow, nestedWindow); w > 0 {
		u.contextWindow = addSaturatingInt(0, w)
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

// ContextWindow returns the model context window recorded in the transcript, or 0 if none was recorded.
func (h *Projection) ContextWindow() int { return h.usage.contextWindow }

// Apply fills the transcript-owned state, without overwriting the runtime's controls or turn error.
func (h *Projection) Apply(s *proto.Session, pane bool) {
	h.ApplyWithPolicy(s, pane, ContextPolicy{})
}

// ApplyWithPolicy fills the transcript-owned state using policy for the model's context window
// and auto-compaction threshold when supplied.
func (h *Projection) ApplyWithPolicy(s *proto.Session, pane bool, policy ContextPolicy) {
	u := &h.usage
	model := u.model
	if !pane && s.Model != "" {
		model = s.Model
	} else if model == "" {
		model = s.Model
	}
	window := 0
	source := proto.ContextSourceRule
	switch {
	case policy.Window > 0:
		window = policy.Window
		if policy.WindowNative {
			source = proto.ContextSourceReported
		}
	case !pane && s.ContextWindow > 0:
		window = int(s.ContextWindow)
		source = proto.ContextSourceReported
	case u.contextWindow > 0:
		window = u.contextWindow
		source = proto.ContextSourceReported
	default:
		window = WindowForModel(model, 0)
	}
	autoCompact := window
	if policy.AutoCompactAt > 0 && policy.AutoCompactAt < autoCompact {
		autoCompact = policy.AutoCompactAt
	}
	s.ContextTokens, s.ContextWindow = proto.Tokens32(u.contextTokens), proto.Tokens32(window)
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
		Used:          int64(u.contextTokens),
		Window:        int64(window),
		AutoCompactAt: int64(autoCompact),
		Source:        source,
	}
	s.SetContextBreakdown(transcript.BuildContextBreakdown(u.contextStats, u.contextTokens, window, autoCompact, true))
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
	h.FillInfoWithPolicy(info, ContextPolicy{})
}

func (h *Projection) FillInfoWithPolicy(info map[string]any, policy ContextPolicy) {
	var s proto.Session
	if model, _ := info["model"].(string); model != "" {
		s.Model = model
	}
	h.ApplyWithPolicy(&s, false, policy)
	info["context_tokens"], info["context_window"], info["output_tokens"] = h.usage.contextTokens, int(s.ContextWindow), h.usage.outputTokens
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
