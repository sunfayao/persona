package agents

// How an A2A message tells its recipient to reach Persona's tools: a recipient with the Persona MCP tools is told the
// tool alone; one without them (#13893) its node's executable, named as the task handoff names it (task_handoff.go),
// since `personad` is often not on such an Agent's PATH. The A2A tools bind a terminal caller by its pane or Codex
// thread (node/clicall.go Caller), whatever the program is called.

import (
	"context"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// a2aEntry is how an A2A message's recipient reaches the A2A tools, as prompts/agents/a2a_message takes it: the
// Persona MCP tools, or its node's program (a JSON string) run from a terminal the node verifies (its pane, a Codex
// thread). Neither for a runtime's session (its harness wires the A2A tools in, or it answers through chat) and when
// the node names no program: the message then names the tool and the reply line that stands in for it.
type a2aEntry struct {
	mcp        bool
	executable string
}

func (e a2aEntry) lay(m *prompts.A2AMessage) { m.MCP, m.Executable = e.mcp, e.executable }

// a2aSessionEntry is the entry of running session sess on machine. Its node is asked for its program only for a
// terminal session without the MCP tools.
func (s *Domain) a2aSessionEntry(ctx context.Context, machine string, sess proto.Session) a2aEntry {
	mcp, terminal := SessionEntry(sess)
	if mcp || !terminal {
		return a2aEntry{mcp: mcp}
	}
	return a2aEntry{executable: s.nodes.Executable(ctx, machine)}
}

// a2aLaunchEntry is the same for the first message of an Agent an A2A task starts with launcher on machine, whose
// harness is not known before it starts: the MCP tools when the launcher is known, without asking the computer, to
// run Claude Code, which Persona starts with them (the node's --mcp-config); the node's program otherwise.
func (s *Domain) a2aLaunchEntry(ctx context.Context, machine, launcher string) a2aEntry {
	if s.launches.UsesClaude(ctx, launcher) {
		return a2aEntry{mcp: true}
	}
	return a2aEntry{executable: s.nodes.Executable(ctx, machine)}
}

// a2aTaskMessage is event e of task t from from, in language, as prompts/agents/a2a_message takes it, without the
// recipient's entry: the caller adds that once it knows the recipient (a2aSessionEntry, a2aLaunchEntry).
func a2aTaskMessage(t *Task, e *Event, from Ref, language string) prompts.A2AMessage {
	state := e.State
	if state == "" {
		state = t.State
	}
	return prompts.A2AMessage{ID: t.ID, Sender: a2aMessageSource(from).Title, State: state, Request: e.Kind == "message",
		Text: e.Text, Language: language}
}

// a2aRelay is m as its recipient receives it: rendered, then marked with its source and its delivery id.
func a2aRelay(m prompts.A2AMessage, from Ref, id string) string {
	return prompts.RelayMessageText(prompts.RenderA2AMessage(m), a2aMessageSource(from), id)
}

// SessionEntry is how running session c reaches its own Persona tools: whether it has the Persona MCP tools, and
// whether it calls them from a terminal the node verifies. A Codex app-server thread can identify its caller through
// CODEX_THREAD_ID even when it has no terminal pane.
func SessionEntry(c proto.Session) (mcp, terminal bool) {
	terminal = c.SourceKind == "" || c.SourceKind == proto.SourceClaudeTerminal ||
		c.Agent == "codex" && c.SourceThreadID != "" && (c.SourceKind == "codex_app_server" || c.SourceKind == "codex_history")
	mcp = terminal && c.Agent == "claude"
	return mcp, terminal
}
