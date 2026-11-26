package textfmt

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Transcript is a conversation's recent items for a model to read (agent_read, conversation_read): the
// messages, tool calls in one line each, failed results, cut to at most the last 40 000 bytes at a rune boundary.
// linkToItem optionally supplies a link beside each message.
func Transcript(head string, items []proto.Item, linkToItem func(string) string) string {
	var b strings.Builder
	b.WriteString(head + "\n\n")
	for _, it := range items {
		switch it.Kind {
		case proto.KindText:
			who := "Agent"
			if it.Role == "user" {
				who = "User"
				if it.From != "" {
					who = "Message from " + it.From
				}
			}
			if linkToItem != nil {
				if l := linkToItem(it.ID); l != "" {
					who += " (" + l + ")"
				}
			}
			text := it.Text
			if it.Role == "assistant" {
				text = wire.AssistantCardText(text)
			}
			fmt.Fprintf(&b, "%s: %s\n\n", who, Clip(text, 4000))
		case proto.KindThinking:
			if t := strings.TrimSpace(it.Text); t != "" {
				fmt.Fprintf(&b, "(agent, progress summary) %s\n", Clip(OneLine(t), 400))
			}
		case proto.KindToolUse:
			fmt.Fprintf(&b, "→ %s %s\n", it.ToolName, Clip(OneLine(ToolSummary(it.ToolName, it.ToolInput)), 200))
		case proto.KindToolResult:
			if it.IsError {
				fmt.Fprintf(&b, "  ✗ %s\n", Clip(OneLine(it.Text), 300))
			}
		case proto.KindNotice, proto.KindRecap:
			if it.Text != "" {
				fmt.Fprintf(&b, "(%s)\n", Clip(OneLine(it.Text), 300))
			}
		case proto.KindImage:
			b.WriteString("[image]\n")
		}
	}
	out := b.String()
	if len(out) > 40000 {
		start := len(out) - 40000
		for start < len(out) && !utf8.RuneStart(out[start]) {
			start++
		}
		out = "…(earlier part cut)\n" + out[start:]
	}
	return out
}

// ToolSummary is the one-line gist of a tool call (command, file, url…).
func ToolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	for _, k := range []string{"command", "file_path", "url", "query", "pattern", "description", "prompt", "path"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}
