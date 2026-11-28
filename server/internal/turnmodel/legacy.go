// Legacy decoder: reads an item's canonical facets first, falls back to the legacy fields older nodes still send.
// This is the single decoder the manager and channel packages read; it replaces scattered string literals and
// CLI-specific branches in assistantwire, assistant and channel.
package turnmodel

import (
	"encoding/json"
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// NoticeCode returns the item's typed notice code from its facets, or "" when the item has none.
func NoticeCode(it proto.Item) string {
	if it.Facets != nil && it.Facets.Notice != nil {
		return it.Facets.Notice.Code
	}
	return ""
}

// HasNoticeCode reports whether the item's facets carry the given notice code.
func HasNoticeCode(it proto.Item, code string) bool {
	return NoticeCode(it) == code
}

// ToolKind returns the canonical ToolKind from the item's facets, or classifies its ToolName with KindOf when it
// has no facets. An empty facets.Tool is a pre-turn-model node; the classification still works for the names that
// have been stable across CLIs.
func ToolKind(it proto.Item) string {
	if it.Facets != nil && it.Facets.Tool != nil && it.Facets.Tool.Kind != "" {
		return it.Facets.Tool.Kind
	}
	return KindOf(NameOf(it.ToolName))
}

// SubagentAgentType reads the agent_type from facets.Subagent when present, else returns the legacy subagent_type
// field parsed from ToolInput JSON. Used by channels_runs.go and feishu_steps.go for the subtask card's subtitle.
func SubagentAgentType(it proto.Item) string {
	if it.Facets != nil && it.Facets.Subagent != nil && it.Facets.Subagent.AgentType != "" {
		return it.Facets.Subagent.AgentType
	}
	var raw struct {
		SubagentType string `json:"subagent_type"`
	}
	if err := json.Unmarshal(it.ToolInput, &raw); err != nil {
		return ""
	}
	return raw.SubagentType
}

// ToolInputField returns the value of the named ToolInput JSON field as a trimmed string, or "" when missing.
// Used where older CLI mappings wrote the same semantic input under different keys (Codex's "change", "description"…).
func ToolInputField(it proto.Item, field string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(it.ToolInput, &raw); err != nil {
		return ""
	}
	v, ok := raw[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// IsTaskOrAgentTool reports whether this tool call spawns a subagent or background task. Covers the canonical tool
// kinds "agent" and "task" plus the legacy exact names Codex and Claude Code used before the turn-model.
func IsTaskOrAgentTool(it proto.Item) bool {
	switch ToolKind(it) {
	case proto.ToolKindAgent, proto.ToolKindTask:
		return true
	}
	// Legacy: older nodes without facets wrote "Task" / "Agent" and the node mapped Codex's equivalent names there.
	return it.ToolName == "Task" || it.ToolName == "Agent"
}

// IsEditLike reports whether the tool mutates a file.
func IsEditLike(it proto.Item) bool {
	switch ToolKind(it) {
	case proto.ToolKindEdit, proto.ToolKindWrite, proto.ToolKindDelete, proto.ToolKindMove:
		return true
	}
	return false
}

// IsShellLike reports whether the tool executes a command.
func IsShellLike(it proto.Item) bool {
	return ToolKind(it) == proto.ToolKindExecute
}

const (
	LegacyInterrupted = "已中断"    // i18n-ignore: legacy transcript marker
	LegacyRetrying    = "（正在重试）" // i18n-ignore: legacy transcript marker
)

// ---- notice recognisers ------------------------------------------------------------
// These are the single place that a notice's meaning is decided, reading the typed code first and falling back to
// the legacy text literals older nodes still send. Assistant and channel callers must not re-match the text.

// IsInterruptedNotice reports whether the item is a user-interrupted notice.
func IsInterruptedNotice(it proto.Item) bool {
	if HasNoticeCode(it, proto.NoticeCodeInterrupted) {
		return true
	}
	return it.Kind == proto.KindNotice && it.Text == LegacyInterrupted
}

// IsAPIRetryNotice reports whether the notice says the model API is retrying.
func IsAPIRetryNotice(it proto.Item) bool {
	if HasNoticeCode(it, proto.NoticeCodeRetrying) {
		return true
	}
	return it.Kind == proto.KindNotice && strings.HasSuffix(strings.TrimSpace(it.Text), LegacyRetrying)
}

// IsErrorNotice reports whether the notice carries a typed api_error code, or — as a legacy fallback — starts
// with one of the APIErrorMarkers that assistantwire also uses.
func IsErrorNotice(it proto.Item) bool {
	if HasNoticeCode(it, proto.NoticeCodeAPIError) || HasNoticeCode(it, proto.NoticeCodeRuntimeError) {
		return true
	}
	return false
}

// IsTranscriptUnlinked identifies the pane placeholder, including the legacy item identity.
func IsTranscriptUnlinked(it proto.Item) bool {
	if it.Facets != nil && it.Facets.Notice != nil {
		return it.Facets.Notice.Args["reason"] == "transcript_unlinked"
	}
	return it.Kind == proto.KindNotice && it.ID == "agy#0"
}
