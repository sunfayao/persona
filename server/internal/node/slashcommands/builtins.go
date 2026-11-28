package slashcommands

import (
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// These documented terminal commands are absent from the headless protocols. Dynamic rows
// still come from the CLI itself. Codex CLI exposes skills through $name, not /name.
// https://code.claude.com/docs/en/commands
// https://antigravity.google/docs/cli/reference
// https://github.com/openai/codex/blob/rust-v0.171.0/codex-rs/tui/src/slash_command.rs
func builtins(agent string) []proto.SlashCommand {
	var lines string
	switch agent {
	case "claude":
		lines = `help Show commands and keyboard shortcuts
compact Summarize the conversation to free context
clear Start a fresh conversation
context Inspect current context usage
review Review changes
init Create project instructions
cost Show token usage and API cost
doctor Check the Claude Code installation
memory Manage project memory
model Choose the active model
status Inspect the session and configuration
usage Inspect account limits
diff Inspect changed files
tasks Inspect background tasks
mcp Manage MCP servers
config Open settings
effort Choose reasoning effort
resume Reopen a conversation
permissions Manage tool permissions
rename Rename the conversation
rewind Revisit an earlier point in the conversation
plan Enter plan mode`
	case "codex":
		lines = codexBuiltinLines
	case "agy":
		lines = `help Show commands and keyboard shortcuts
context Inspect current context usage
config Open Antigravity settings
model Choose the active model
mcp Manage MCP servers
agents Inspect and switch agents
artifact Open the artifact review panel
btw Ask a side question
clear Reset the conversation
diff Inspect workspace changes
tasks Inspect background tasks
usage Inspect usage
effort Choose reasoning effort
resume Reopen a conversation
permissions Manage tool permissions
goal Set a goal
compact Summarize the conversation to free context
schedule Schedule an instruction
browser Delegate a task to the browser agent
plan Plan before implementing
grill-me Explore a plan through an interview
teamwork Run a team of agents
learn Turn a correction into reusable guidance
boost Apply deeper multi-agent reasoning`
	case "traex":
		lines = `review Review working tree changes
review-branch Review changes against a branch
review-commit Review changes introduced by a commit
init Create an AGENTS.md file
compact Summarize the conversation to free context
logout Sign out of TRAE CLI
goal Set a goal`
	}
	var rows []proto.SlashCommand
	for line := range strings.SplitSeq(lines, "\n") {
		name, description, ok := strings.Cut(line, " ")
		if ok {
			rows = append(rows, proto.SlashCommand{Name: name, Description: description, Source: proto.SlashBuiltin})
		}
	}
	return rows
}

const codexBuiltinLines = `model Choose a model and reasoning effort
permissions Choose tool and filesystem permissions
review Review changes
compact Summarize the conversation to free context
status Inspect the current session
diff Inspect working tree changes
skills List available skills
mcp Inspect MCP tools and connections
resume Reopen a conversation
statusline Configure the terminal status line
rename Rename the current thread
new Start a new conversation
clear Clear the terminal and start a new conversation
fork Copy the current conversation into a new thread
init Create an AGENTS.md file
plan Enter plan mode
mention Add a file reference
copy Copy the latest assistant response
feedback Share diagnostic feedback
config Configure Codex
hooks Inspect lifecycle hooks
plugins Browse plugins
apps Manage connected apps
goal Set or inspect the current goal
worktree Create or switch a worktree
presets Open saved Codex presets
subagents Switch between subagents
keymap Configure terminal shortcuts
vim Toggle Vim-style editing
ide Include the current IDE context
archive Archive the current thread
export Export the conversation
theme Choose a terminal theme
ps Inspect background terminals
stop Stop background terminals
exit Exit Codex
quit Exit Codex
experimental Configure experimental features`

// CodexTerminalCommand recognizes the built-ins handled in a terminal even when its thread also has an app-server.
func CodexTerminalCommand(word string) bool {
	name, ok := strings.CutPrefix(word, "/")
	if !ok || name == "compact" {
		return false
	}
	for line := range strings.SplitSeq(codexBuiltinLines, "\n") {
		command, _, _ := strings.Cut(line, " ")
		if name == command {
			return true
		}
	}
	return false
}
