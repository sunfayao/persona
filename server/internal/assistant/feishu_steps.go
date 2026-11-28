package assistant

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/sunfayao/persona/server/internal/feishubot"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

// botStep is one line of the progress card for a tool call, in l: the verb, then what it works on (code when it is a
// command, file or pattern) and a note.
func botStep(l i18n.Lang, it proto.Item) string {
	s := stepOf(l, it)
	line := s.verb
	switch {
	case s.target == "":
	case s.plain:
		line += " · " + feishubot.EscapeMarkdown(s.target)
	default:
		line += " " + feishubot.Code(s.target)
	}
	return withNote(line, feishubot.EscapeMarkdown(s.note))
}

// botStepParts is what a tool call did, in l: the verb ("" when it is no tool call) and what it did it to.
func botStepParts(l i18n.Lang, it proto.Item) (string, string) {
	s := stepOf(l, it)
	return s.verb, strings.TrimSpace(withNote(s.target, s.note))
}

// withNote puts a note after s: a space, none before a full-width bracket.
func withNote(s, note string) string {
	switch {
	case note == "":
		return s
	case strings.HasPrefix(note, "（"):
		return s + note
	}
	return s + " " + note
}

// step is a tool call said plainly: a verb, what it works on (plain: words, not code) and a short note after it.
type step struct {
	verb, target, note string
	plain              bool
}

// stepOf reads a tool call into a step. The canonical tool kind is read from facets.Tool.Kind when present;
// the exact tool name (classified by turnmodel.KindOf) is the legacy fallback older nodes still send. Codex's calls
// were already mapped to Claude's names by the transcript layer.
func stepOf(l i18n.Lang, it proto.Item) step {
	if it.Kind != proto.KindToolUse {
		return step{}
	}
	var in map[string]any
	_ = json.Unmarshal(it.ToolInput, &in)
	str := func(k string) string { v, _ := in[k].(string); return strings.TrimSpace(v) }
	num := func(k string) int { v, _ := in[k].(float64); return int(v) }
	t := func(key string, a ...i18n.Args) string { return i18n.T(l, "assistant-feishu."+key, a...) }
	base := func(k string) string {
		if p := str(k); p != "" {
			return path.Base(p)
		}
		return ""
	}
	var s step
	// Exact-name cases that still need their own input keys (TodoWrite's todos, Skill's skill name, Grep/Glob's
	// path/glob distinction, tool kinds the registry groups like "mcp__*"). Everything else routes by canonical kind.
	switch it.ToolName {
	case "TodoWrite":
		s.verb, s.target, s.plain = t("step_todos"), stepNow(in), true
	case "Skill":
		s.verb, s.target = t("step_skill"), str("skill")
	case "Grep", "Glob":
		s.verb, s.target = t("step_search"), str("pattern")
		where, glob := str("path"), ""
		if it.ToolName == "Grep" {
			glob = str("glob")
		}
		switch {
		case where != "" && glob != "":
			s.note = t("step_where_glob", i18n.Args{"where": where, "glob": glob})
		case where != "":
			s.note = t("step_where", i18n.Args{"where": where})
		case glob != "":
			s.note = t("step_glob", i18n.Args{"glob": glob})
		}
	default:
		switch turnmodel.ToolKind(it) {
		case proto.ToolKindExecute:
			s.verb = t("step_run")
			if b, _ := in["run_in_background"].(bool); b {
				s.verb = t("step_run_background")
			}
			if d := str("description"); d != "" {
				s.target, s.plain = d, true
			} else {
				s.target = firstLine(str("command"))
			}
		case proto.ToolKindRead:
			s.verb, s.target = t("step_read"), base("file_path")
			if from, n := num("offset"), num("limit"); from > 0 || n > 0 {
				from = max(from, 1)
				if n > 0 {
					s.note = t("step_lines", i18n.Args{"from": from, "to": from + n - 1})
				} else {
					s.note = t("step_lines_from", i18n.Args{"from": from})
				}
			}
		case proto.ToolKindEdit, proto.ToolKindWrite, proto.ToolKindDelete:
			s.verb, s.target = t("step_edit"), base("file_path")
			if s.target == "" {
				s.target = base("notebook_path")
			}
			switch str("change") { // Codex's file changes, still written as-is in the legacy adapter
			case "add":
				s.verb = t("step_create")
			case "delete":
				s.verb = t("step_delete")
			}
			if turnmodel.ToolKind(it) == proto.ToolKindWrite && str("change") == "" {
				s.verb = t("step_write")
			}
		case proto.ToolKindFetch:
			s.verb, s.target = t("step_open_web"), str("url")
		case proto.ToolKindWebSearch:
			s.verb, s.target = t("step_search_web"), str("query")
		case proto.ToolKindAgent, proto.ToolKindTask:
			s.verb, s.target, s.plain = t("step_subtask"), str("description"), true
			if a := turnmodel.SubagentAgentType(it); a != "" {
				s.note = t("step_agent_type", i18n.Args{"type": a})
			}
		case proto.ToolKindMcp, proto.ToolKindOther:
			tool := it.ToolName
			if _, mcp, ok := turnmodel.SplitMCP(tool); ok {
				tool = mcp
			} else if i := strings.LastIndex(tool, "__"); i > 4 && strings.HasPrefix(tool, "mcp__") {
				tool = tool[i+2:]
			}
			s.verb = tool
			if key := stepMCP[tool]; key != "" {
				s.verb = t(key)
			}
			for _, k := range []string{"query", "q", "title", "to", "subject", "command", "cmd", "path", "file_path", "url", "name", "id"} {
				if v := str(k); v != "" {
					s.target = firstLine(v)
					break
				}
			}
		default:
			s.verb = it.ToolName
		}
	}
	s.target = clip(oneLine(s.target), 60)
	return s
}

// stepMCP is the verb for Persona's own MCP tools (internal/prompts/mcp), by the tool's name.
var stepMCP = map[string]string{
	"mail_search": "step_mail_search",
	"mail_read":   "step_mail_read",
	"mail_send":   "step_mail_send",
}

// stepNow is what a to-do list (TodoWrite's todos) is doing now: its in-progress item.
func stepNow(in map[string]any) string {
	list, _ := in["todos"].([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if st, _ := m["status"].(string); st != "in_progress" {
			continue
		}
		for _, k := range []string{"activeForm", "content"} {
			if v, _ := m[k].(string); strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return ""
}
