package codex

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/shellcmd"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// codexShellArgv splits the App's command line ("/bin/bash -lc 'git status'") into the argv a rollout carries, so the
// transcript reader drops the login shell around it as it does for a rollout's (transcript.shellCommand). The command is
// one shell word, unquoted as a POSIX shell does; one that is not (several words, an open quote) is kept whole.
func shellArgv(command string) []string {
	shells := [...]string{"sh", "bash", "zsh", "dash", "ksh", "fish"}
	fields := strings.SplitN(command, " ", 3)
	if len(fields) != 3 || (fields[1] != "-lc" && fields[1] != "-c") || !slices.Contains(shells[:], path.Base(fields[0])) {
		return []string{command}
	}
	if inner, ok := shellcmd.Word(fields[2]); ok {
		return []string{fields[0], fields[1], inner}
	}
	return []string{command}
}

// DecodeThreadItem adapts the app-server's item wire format to the native record shared with rollouts.
// An in-progress item stays on the live stream until the CLI records its completion.
func DecodeThreadItem(kind string, raw []byte) map[string]any {
	var v struct {
		ID      string `json:"id"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Path string `json:"path"`
		} `json:"content"`
		Text             string          `json:"text"`
		Phase            string          `json:"phase"`
		Summary          []string        `json:"summary"`
		Command          string          `json:"command"`
		Source           string          `json:"source"`
		ProcessID        string          `json:"processId"`
		Kind             string          `json:"kind"`
		AgentThreadID    string          `json:"agentThreadId"`
		AgentPath        string          `json:"agentPath"`
		Model            *string         `json:"model"`
		ReasoningEffort  *string         `json:"reasoningEffort"`
		ReceiverThreads  []string        `json:"receiverThreadIds"`
		AgentStates      json.RawMessage `json:"agentsStates"`
		TaskNotification json.RawMessage `json:"taskNotification"`
		Status           string          `json:"status"`
		AggregatedOutput string          `json:"aggregatedOutput"`
		ExitCode         *int            `json:"exitCode"`
		DurationMs       *int64          `json:"durationMs"`
		Server           string          `json:"server"`
		Tool             string          `json:"tool"`
		Arguments        json.RawMessage `json:"arguments"`
		Result           *struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Data     string `json:"data"`
				MimeType string `json:"mimeType"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error     json.RawMessage `json:"error"`
		Path      string          `json:"path"`
		SavedPath string          `json:"savedPath"`
		Changes   []struct {
			Path string `json:"path"`
			Kind struct {
				Type string `json:"type"`
			} `json:"kind"`
			Diff string `json:"diff"`
		} `json:"changes"`
		Query  string `json:"query"`
		Action *struct {
			Type    string   `json:"type"`
			URL     string   `json:"url"`
			Queries []string `json:"queries"`
		} `json:"action"`
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Domain  string `json:"domain"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Status == "inProgress" {
		return nil
	}
	item := map[string]any{"id": v.ID}
	switch kind {
	case "userMessage":
		var content []map[string]string
		for _, c := range v.Content {
			switch c.Type {
			case "text":
				content = append(content, map[string]string{"type": "text", "text": c.Text})
			case "localImage":
				content = append(content, map[string]string{"type": "local_image", "path": c.Path})
			}
		}
		item["type"], item["content"] = "UserMessage", content
	case "agentMessage":
		item["type"], item["content"], item["phase"] = "AgentMessage", []map[string]string{{"type": "text", "text": v.Text}}, v.Phase
	case "plan":
		item["type"], item["text"] = "Plan", v.Text
	case "reasoning":
		item["type"], item["summary_text"] = "Reasoning", v.Summary
	case "commandExecution":
		item["type"], item["command"], item["status"], item["aggregated_output"] = "CommandExecution", shellArgv(v.Command), v.Status, v.AggregatedOutput
		item["source"], item["process_id"], item["arguments"] = v.Source, v.ProcessID, v.Arguments
		if v.ExitCode != nil {
			item["exit_code"] = *v.ExitCode
		}
		if v.DurationMs != nil && *v.DurationMs >= 0 {
			item["duration"] = map[string]int64{"secs": *v.DurationMs / 1000, "nanos": *v.DurationMs % 1000 * 1_000_000}
		}
	case "fileChange":
		changes := map[string]map[string]string{}
		for _, c := range v.Changes {
			changes[c.Path] = map[string]string{"type": c.Kind.Type, "unified_diff": c.Diff}
		}
		item["type"], item["changes"], item["status"] = "FileChange", changes, v.Status
	case "mcpToolCall":
		item["type"], item["server"], item["tool"], item["arguments"], item["status"] = "McpToolCall", v.Server, v.Tool, v.Arguments, v.Status
		if v.Result != nil {
			item["result"] = v.Result
		}
		if len(v.Error) > 0 {
			item["error"] = v.Error
		}
	case "imageView":
		item["type"], item["path"] = "ImageView", v.Path
	case "imageGeneration": // the picture it made, shown as a rollout shows an image it looked at
		if v.SavedPath == "" {
			return nil
		}
		item["type"], item["path"] = "ImageView", v.SavedPath
	case "webSearch":
		item["type"], item["kind"], item["query"], item["results"] = "Extension", "web.search", v.Query, v.Results
		if v.Action != nil {
			item["action"] = v.Action
		}
	case "contextCompaction":
		item["type"] = "ContextCompaction"
	case "terminalInteraction":
		item["type"], item["process_id"], item["command"] = "TerminalInteraction", v.ProcessID, v.Command
	case "subAgentActivity":
		if v.AgentThreadID == "" {
			return nil
		}
		item["type"], item["kind"], item["agent_thread_id"], item["agent_path"] = "SubAgentActivity", v.Kind, v.AgentThreadID, v.AgentPath
		item["model"], item["reasoning_effort"] = v.Model, v.ReasoningEffort
	case "collabAgentToolCall":
		item["type"], item["tool"], item["status"] = "CollabAgentToolCall", v.Tool, v.Status
		item["receiver_thread_ids"], item["agents_states"] = v.ReceiverThreads, v.AgentStates
	case "taskNotification":
		item["type"], item["task_notification"] = "TaskNotification", v.TaskNotification
	default:
		return nil
	}
	return item
}

// ThreadItemRecord normalizes a v2 item for the transcript mapper shared by live and indexed sources.
func ThreadItemRecord(at time.Time, turn, kind string, raw json.RawMessage) (transcript.CodexRecord, bool) {
	item := DecodeThreadItem(kind, raw)
	if item == nil {
		return transcript.CodexRecord{}, false
	}
	payload, _ := json.Marshal(map[string]any{"type": "item_completed", "turn_id": turn, "item": item})
	return transcript.CodexRecord{Timestamp: at, Type: "event_msg", Payload: payload}, true
}
