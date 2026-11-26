package codex

import (
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/shellcmd"
)

// A Codex App chat whose rollout is gone (codex_appdb.go) keeps its history in <CODEX_HOME>/thread_history_<n>.sqlite:
// thread_turns, and thread_items holding the app-server's ThreadItems (v2: userMessage, agentMessage, commandExecution…).
// Its transcript is that history written back as rollout lines — session_meta, then per finished turn task_started, an
// item_completed per item, and task_complete (with its error when it failed) or turn_aborted — so the transcript reader,
// the clients and every check on a rollout read it as any other (#3305). The file is
// <state dir>/codex-app-history/<home hash>-<thread>.jsonl. It grows as a rollout does: the turns finished since it was
// last written are appended (the last turn while it still runs is left for later; one that never finished but has turns
// after it is written as interrupted), so a reader goes on where it was. <file>.mark records what was written; a file
// unlike its mark, or whose last turn is no longer in the database as it was, is written again from the start (and
// removed when no turn is left finished). It is brought up to date when the transcript is read (file) and, while the chat
// is watched, about once a second (pumpWatch), never by the list; one writer at a time, across processes too
// (<file>.lock: the others wait for its write). The database is read while the App has it closed too (codexDBTarget).
// Without sqlite3 or the history database there is none, as before.

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

// codexRolloutItem turns one of the app-server's ThreadItems (v2, camelCase) into the item a rollout's item_completed
// carries (transcript/codex.go codexItem); nil for a kind the chat does not show (a sleep, a subagent's progress) and for
// an item still running.
func RolloutItem(kind string, raw []byte) map[string]any {
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
	case "reasoning":
		item["type"], item["summary_text"] = "Reasoning", v.Summary
	case "commandExecution":
		item["type"], item["command"], item["status"], item["aggregated_output"] = "CommandExecution", shellArgv(v.Command), v.Status, v.AggregatedOutput
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
	default:
		return nil
	}
	return item
}
