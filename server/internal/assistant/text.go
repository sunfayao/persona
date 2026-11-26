package assistant

import (
	"github.com/nwverse/persona/server/internal/deeplink"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/textfmt"
)

// Formatting for tool results and the hub's logs, the same the hub writes with (one copy, internal/textfmt).
var (
	clip          = textfmt.Clip
	oneLine       = textfmt.OneLine
	firstLine     = textfmt.FirstLine
	firstNonEmpty = textfmt.FirstNonEmpty
	planTitle     = textfmt.PlanTitle
	ago           = textfmt.Ago
	short         = textfmt.Short
	agentLabel    = textfmt.AgentLabel
	stateNames    = textfmt.StateNames
	toolSummary   = textfmt.ToolSummary
)

func transcriptText(head, link string, items []proto.Item) string {
	return textfmt.Transcript(head, items, func(id string) string { return itemLink(link, id) })
}

// itemLink is the link to one item of a conversation whose page is link, "" when either is.
func itemLink(link, id string) string {
	if link == "" || id == "" {
		return ""
	}
	return deeplink.At(link, id)
}

func clipRunesTo(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
