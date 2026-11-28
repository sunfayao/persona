package agy

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
	"github.com/sunfayao/persona/server/internal/transcript"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

type nativeCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type toolArgs map[string]json.RawMessage

func (a toolArgs) first(keys ...string) json.RawMessage {
	for _, key := range keys {
		if raw := a[key]; raw != nil && string(raw) != "null" {
			return raw
		}
	}
	return nil
}

func (a toolArgs) text(keys ...string) string {
	var v string
	_ = json.Unmarshal(a.first(keys...), &v)
	return v
}

func (a toolArgs) count(key string) int {
	var n int
	_ = json.Unmarshal(a[key], &n)
	return n
}

func (a toolArgs) flag(key string) bool {
	var b bool
	_ = json.Unmarshal(a[key], &b)
	return b
}

var bareTool = lazyre.New(`^[a-z][a-z0-9_]*$`)

func toolName(name string) string {
	switch name {
	case "run_command", "bash":
		return "Bash"
	case "view_file", "read_file", "read":
		return "Read"
	case "write_to_file", "write_file", "write":
		return "Write"
	case "replace_file_content", "edit_file", "edit":
		return "Edit"
	case "grep_search", "grep":
		return "Grep"
	case "find_by_name", "glob":
		return "Glob"
	case "search_web", "web_search":
		return "WebSearch"
	case "read_url_content", "web_fetch":
		return "WebFetch"
	case "ask_question":
		return "AskUserQuestion"
	case "invoke_subagent":
		return "Agent"
	case "todo_write", "update_steps":
		return "TodoWrite"
	case "schedule":
		return "Schedule"
	case "multi_edit":
		return "MultiEdit"
	case "ls":
		return "LS"
	case "":
		return "tool"
	default:
		return compat.CanonicalMCPToolName(name)
	}
}

func mapCall(call *nativeCall, it *proto.Item) toolArgs {
	name := toolName(call.Name)
	var args toolArgs
	_ = json.Unmarshal(call.Args, &args)
	it.Facets.Native.Name = call.Name
	title := textfmt.Clip(args.text("toolSummary"), 512)
	var input any = args
	if args == nil {
		input = struct{}{}

	}
	asMap := func(kvs ...string) map[string]any {
		out := make(map[string]any, len(kvs)/2)
		for i := 0; i < len(kvs); i += 2 {
			out[kvs[i]] = kvs[i+1]
		}
		return out
	}
	var locations []proto.ToolLocation
	switch call.Name {
	case "run_command":
		m := asMap("command", args.text("CommandLine", "command"), "description", args.text("toolSummary", "Cwd"))
		if args.flag("IsDaemon") {
			m["run_in_background"] = true
		}
		input = m
	case "view_file", "read_file":
		path := args.text("AbsolutePath", "Path", "file_path")
		m := asMap("file_path", path)
		start, end := args.count("StartLine"), args.count("EndLine")
		if start > 0 {
			m["offset"] = start
		}
		if end > 0 && end >= max(start, 1) {
			m["limit"] = end - max(start, 1) + 1
		}
		if path != "" {
			locations = []proto.ToolLocation{{Path: path, Line: start, EndLine: end}}
		}
		input = m
	case "write_to_file", "write_file":
		input = asMap("file_path", args.text("TargetFile", "Path", "file_path"), "content", args.text("CodeContent", "Content", "content"))
	case "replace_file_content", "edit_file":
		input = asMap("file_path", args.text("TargetFile", "Path", "file_path"), "old_string", args.text("TargetContent", "Target", "old_string"),
			"new_string", args.text("ReplacementContent", "Replacement", "new_string"))
	case "grep_search":
		input = asMap("pattern", args.text("Query", "Pattern", "pattern"), "path", args.text("SearchPath", "Path", "path"))
	case "find_by_name":
		input = asMap("pattern", args.text("Pattern", "pattern"), "path", args.text("SearchDirectory", "Path", "path"))
	case "search_web":
		m := asMap("query", args.text("query", "Query"))
		if domain := args.text("domain"); domain != "" {
			m["allowed_domains"] = []string{domain}
		}
		input = m
	case "read_url_content":
		input = asMap("url", args.text("Url", "url"))
	case "invoke_subagent":
		desc, prompt := args.text("toolSummary"), args.text("Subagents")
		if desc == "" {
			desc = "subagent"
		}
		if raw := args.first("Subagents"); len(raw) > 0 && raw[0] != '"' {
			var buf bytes.Buffer
			if json.Compact(&buf, raw) == nil {
				prompt = buf.String()
			}
		}
		input = asMap("description", desc, "prompt", prompt)
	case "update_steps":
		input, it.Facets.Plan = mapPlan(args["steps"])
	case "ask_question":
		input, it.Facets.Interaction = mapQuestions(args["questions"])
	case "manage_task":
		switch strings.ToLower(args.text("Action")) {
		case "status":
			name = "TaskOutput"
		case "list":
			name = "TaskList"
		case "kill":
			name = "TaskStop"
		}
		if name != call.Name {
			input = asMap("task_id", args.text("TaskId"))
		}
	case "schedule":
		m := asMap("prompt", args.text("Prompt"))
		for _, keys := range [][2]string{{"delay_s", "DurationSeconds"}, {"cron", "CronExpression"}, {"condition", "TimerCondition"}, {"max_runs", "MaxIterations"}} {
			if v := args.first(keys[1]); v != nil {
				m[keys[0]] = v
			}
		}
		input = m
	}
	if name == "persona_call" || name == "mcp__persona__persona_call" {
		if target := strings.TrimSpace(args.text("name")); bareTool.MatchString(target) {
			name, input = "mcp__persona__"+target, struct{}{}
			raw := bytes.TrimSpace(args["arguments"])
			if len(raw) > 0 && raw[0] == '"' {
				raw = bytes.TrimSpace([]byte(args.text("arguments")))
			}
			if len(raw) > 0 && raw[0] == '{' && json.Valid(raw) {
				input = json.RawMessage(raw)
			}
		}
	}
	if raw := bytes.TrimSpace(call.Args); len(raw) > 0 && raw[0] == '[' && json.Valid(raw) {
		wrapped := append(append([]byte(`{"args":`), raw...), '}')
		input = json.RawMessage(raw)
		if clipped := transcript.ClipToolInput(wrapped); len(clipped) < len(wrapped) {
			input = json.RawMessage(clipped)
		}
	}
	encoded, _ := json.Marshal(input)
	it.ToolName, it.ToolInput = name, transcript.ClipToolInput(encoded)
	it.Facets.Tool = &proto.ToolFacet{Kind: turnmodel.KindOf(name), Name: name, Title: title, Locations: locations}
	if server, tool, ok := turnmodel.SplitMCP(name); ok {
		it.Facets.Tool.Mcp = &proto.McpRef{Server: server, Tool: tool}
	}
	return args
}

func mapPlan(raw json.RawMessage) (any, *proto.PlanFacet) {
	var steps []struct{ Title, Status string }
	if json.Unmarshal(raw, &steps) != nil || steps == nil {
		return struct{}{}, nil
	}
	entries := make([]proto.PlanEntry, 0, len(steps))
	legacy := make([]map[string]string, 0, len(steps))
	for _, s := range steps {
		status := strings.ToLower(s.Status)
		switch status {
		case "done":
			status = proto.PlanEntryStatusCompleted
		case "canceled":
			status = proto.PlanEntryStatusCancelled
		case "":
			status = proto.PlanEntryStatusPending
		}
		legacyStatus := status
		switch status {
		case proto.PlanEntryStatusPending, proto.PlanEntryStatusInProgress, proto.PlanEntryStatusCompleted:
		case proto.PlanEntryStatusSkipped, proto.PlanEntryStatusCancelled, proto.PlanEntryStatusFailed:
			legacyStatus = proto.PlanEntryStatusCompleted
		default:
			legacyStatus = proto.PlanEntryStatusPending
		}
		entries = append(entries, proto.PlanEntry{Title: s.Title, Status: status, ActiveForm: s.Title})
		legacy = append(legacy, map[string]string{"content": s.Title, "status": legacyStatus, "activeForm": s.Title})
	}
	return map[string]any{"todos": legacy}, &proto.PlanFacet{Mode: proto.TurnPlanModeReplace, Entries: entries}
}

func mapQuestions(raw json.RawMessage) (any, *proto.InteractionFacet) {
	var native []struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Multi    bool     `json:"is_multi_select"`
	}
	if json.Unmarshal(raw, &native) != nil || native == nil {
		return struct{}{}, nil
	}
	legacy := make([]map[string]any, 0, len(native))
	questions := make([]proto.InteractionQuestion, 0, len(native))
	for _, q := range native {
		options := make([]proto.InteractionOption, 0, len(q.Options))
		for _, o := range q.Options {
			options = append(options, proto.InteractionOption{Label: o})
		}
		questions = append(questions, proto.InteractionQuestion{Question: q.Question, Options: options, MultiSelect: q.Multi})
		legacy = append(legacy, map[string]any{"question": q.Question, "header": "", "options": options, "multiSelect": q.Multi})
	}
	return map[string]any{"questions": legacy}, &proto.InteractionFacet{Kind: proto.InteractionKindQuestion, Status: proto.InteractionStatusPending, Questions: questions}
}

var answerHeader = lazyre.New(`(?m)^A([1-9][0-9]*):[ \t]*`)

func answers(text string, questions []proto.InteractionQuestion) map[string]string {
	matches := answerHeader.FindAllStringSubmatchIndex(text, -1)
	out := make(map[string]string)
	for i, m := range matches {
		n, _ := strconv.Atoi(text[m[2]:m[3]])
		if n < 1 || n > len(questions) {
			continue
		}
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[questions[n-1].Question] = textfmt.Clip(strings.TrimSpace(text[m[1]:end]), 1024)
	}
	return out
}
