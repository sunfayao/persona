package assistant

// How an A2A message tells its recipient to reach Persona's tools: a recipient with the Persona MCP tools is told the
// tool alone; one without them (#13893) its node's executable, named as the task handoff names it (task_handoff.go),
// since `personad` is often not on such an Agent's PATH. The A2A tools bind a terminal caller by its pane or Codex
// thread (node/clicall.go a2aCaller), whatever the program is called.

import (
	"context"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
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
func (s *Service) a2aSessionEntry(ctx context.Context, machine string, sess proto.Session) a2aEntry {
	mcp, terminal := sessionEntry(sess)
	if mcp || !terminal {
		return a2aEntry{mcp: mcp}
	}
	return a2aEntry{executable: s.nodeExecutable(ctx, machine)}
}

// a2aLaunchEntry is the same for the first message of an Agent an A2A task starts with launcher on machine, whose
// harness is not known before it starts: the MCP tools when the launcher is known, without asking the computer, to
// run Claude Code, which Persona starts with them (the node's --mcp-config); the node's program otherwise.
func (s *Service) a2aLaunchEntry(ctx context.Context, machine, launcher string) a2aEntry {
	if s.a2aLaunchesClaude(ctx, launcher) {
		return a2aEntry{mcp: true}
	}
	return a2aEntry{executable: s.nodeExecutable(ctx, machine)}
}

// a2aLaunchesClaude: launcher runs Claude Code by its own name or as a Persona-managed provider of that type; false when
// telling would take the computer's launcher list, which a launch does not wait for.
func (s *Service) a2aLaunchesClaude(ctx context.Context, launcher string) bool {
	if assistantKind(launcher) != "" {
		return launcher == "claude"
	}
	m, managed := s.managedNamed(ctx, launcher)
	return managed && m.AgentType() == "claude"
}

// a2aTaskMessage is event e of task t from from, in language, as prompts/agents/a2a_message takes it, without the
// recipient's entry: the caller adds that once it knows the recipient (a2aSessionEntry, a2aLaunchEntry).
func a2aTaskMessage(t *a2aTask, e *a2aEvent, from a2aRef, language string) prompts.A2AMessage {
	state := e.State
	if state == "" {
		state = t.State
	}
	return prompts.A2AMessage{ID: t.ID, Sender: a2aMessageSource(from).Title, State: state, Request: e.Kind == "message",
		Text: e.Text, Language: language}
}

// a2aRelay is m as its recipient receives it: rendered, then marked with its source and its delivery id.
func a2aRelay(m prompts.A2AMessage, from a2aRef, id string) string {
	return prompts.RelayMessageText(prompts.RenderA2AMessage(m), a2aMessageSource(from), id)
}
